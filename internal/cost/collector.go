// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/spool"
)

// Default cadences and windows for the collection loop.
const (
	DefaultScanInterval = 30 * time.Second
	DefaultPollInterval = 3 * time.Second
	DefaultActiveWindow = 15 * time.Minute
)

// Tiered discovery (🎯T1084). The projects tree on the development host is
// 66,598 transcripts in 5,066 directories; listing every directory and
// stat'ing every transcript cost about 0.65 s of CPU per 30 s pass. Every
// FullSweepInterval the scan still does exactly that. Between sweeps a pass
// lists only directories whose own mtime/size changed (an entry was added,
// removed or renamed) and stats only transcripts that were written within
// WarmWindow when last stat'ed, plus every transcript in a re-listed
// directory.
//
// Residue, accepted (🎯T1084, 2026-10-20): appending to a file does not
// change its directory, so a transcript idle for longer than WarmWindow that
// is then appended is not seen until the next full sweep — at most
// FullSweepInterval late. Its usage is delayed, not lost: the tail resumes
// from the stored offset.
const (
	FullSweepInterval = 10 * time.Minute
	WarmWindow        = 24 * time.Hour
)

// scanListings, scanFileStats and scanDirStats count what the scan reads,
// for tests (🎯T1084).
var scanListings, scanFileStats, scanDirStats atomic.Int64

// scanDir is one directory as the scan last listed it.
type scanDir struct {
	info  os.FileInfo // nil when the listing failed: list it again
	items []scanItem  // in ReadDir (name) order, as filepath.WalkDir visits
}

// scanItem is one entry of a listed directory.
type scanItem struct {
	path     string // spelled as filepath.WalkDir would: durable tail keys use it
	abs      string // filepath.Abs(path) when it differs from path, else ""
	dir      bool
	billable bool
	warm     bool // written within the warm horizon when last stat'ed
}

// identity is the file's absolute path, the key that de-duplicates spellings.
func (it *scanItem) identity() string {
	if it.abs != "" {
		return it.abs
	}
	return it.path
}

func sameDirState(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size()
}

// CollectorArgs parameterises NewCollector. Store and ProjectsRoot are
// required. Attribute maps a session id to a jevons worker/thread id
// ("" = unattributed); nil means nothing is attributed. Now is injected
// for deterministic tests.
type CollectorArgs struct {
	Store        *Store
	ProjectsRoot string // ~/.grok/sessions
	Attribute    func(sessionID string) string
	ActiveWindow time.Duration
	Now          func() time.Time
	// UsageSource enables bounded canonical history catch-up over the
	// Claudia socket. Nil leaves that socket poll disabled. Spool polling
	// remains independent of this source.
	UsageSource *UsageSource
	// Replica ingests Claudia-owned sqlpipe usage and receipt tables.
	// The daemon sets this and leaves UsageSource nil.
	Replica *ReplicaSource
}

// UsageSource is Claudia's provider-neutral history API, injected for tests.
type UsageSource struct {
	Discover func(context.Context, claudiawire.GenerationQuery) (*claudiawire.GenerationPage, error)
	Query    func(context.Context, claudiawire.UsageQuery) (*claudiawire.UsagePage, error)
}

// Collector discovers active session transcripts under ProjectsRoot and
// tails them into the Store. It watches the WHOLE projects tree, not
// just registered workers: the runaway fleet in the 2026-07-06 incident
// was invisible precisely because only registered things were looked at.
type Collector struct {
	store        *Store
	projectsRoot string
	attribute    func(string) string
	activeWindow time.Duration
	now          func() time.Time

	// scan is the tree walk the scan loop runs (ScanOnce); tests swap in
	// a blocking walk to prove a slow scan never starves the poll loop.
	scan func() ([]string, error)

	mu              sync.Mutex
	active          []string // JSONL paths from the last scan
	health          CollectorHealth
	usageSource     *UsageSource
	replica         *ReplicaSource
	replayMu        sync.Mutex
	discoveryCursor string
	nextDiscovery   time.Time

	// scanMu serialises ScanOnce, which owns dirs and lastFullScan.
	scanMu       sync.Mutex
	dirs         map[string]*scanDir
	lastFullScan time.Time
}

// CollectorHealth is the collector's own account of its two loops, so
// the monitor's staleness alarm can say WHICH way the collector is
// blind (a poll pass wedged mid-flight, a store error every pass, a
// loop that simply never woke) instead of only that it is.
type CollectorHealth struct {
	// Started bounds warm-up even if Run never gets scheduled.
	Started time.Time `json:"started"`
	// LastPoll is the last fully successful accounting pass; LastScan is
	// the last completed discovery pass. Errors remain separately visible.
	LastPoll time.Time `json:"last_poll"`
	LastScan time.Time `json:"last_scan"`
	// LastFullScan is the last full sweep; passes between sweeps are
	// tiered (🎯T1084) and stamp LastScan only.
	LastFullScan time.Time `json:"last_full_scan,omitempty"`
	// PollStarted / ScanStarted are non-zero while a pass is in flight.
	PollStarted time.Time `json:"poll_started,omitempty"`
	ScanStarted time.Time `json:"scan_started,omitempty"`
	// LastPollErr / LastScanErr are the most recent pass errors ("" when
	// the last pass was clean).
	LastPollErr string `json:"last_poll_err,omitempty"`
	LastScanErr string `json:"last_scan_err,omitempty"`
	// History traversal and replay are independent of recent spool polling.
	// A terminal traversal is weakly consistent, never an atomic inventory.
	HistoryEnabled        bool      `json:"history_enabled"`
	HistoryStarted        time.Time `json:"history_started,omitempty"`
	LastHistoryPass       time.Time `json:"last_history_pass,omitempty"`
	LastHistoryErr        string    `json:"last_history_err,omitempty"`
	LastInventoryComplete time.Time `json:"last_inventory_complete,omitempty"`
	InventoryIncomplete   bool      `json:"inventory_incomplete"`
}

// Describe renders the health as one clause for an alert detail.
func (h CollectorHealth) Describe(now time.Time) string {
	var parts []string
	if !h.PollStarted.IsZero() {
		parts = append(parts, fmt.Sprintf("poll pass in flight for %s", now.Sub(h.PollStarted).Round(time.Second)))
	}
	if !h.ScanStarted.IsZero() {
		parts = append(parts, fmt.Sprintf("scan in flight for %s", now.Sub(h.ScanStarted).Round(time.Second)))
	}
	if h.LastPollErr != "" {
		parts = append(parts, "last poll error: "+h.LastPollErr)
	}
	if h.LastScanErr != "" {
		parts = append(parts, "last scan error: "+h.LastScanErr)
	}
	if h.HistoryEnabled {
		if h.LastHistoryErr != "" {
			parts = append(parts, "history catch-up error: "+h.LastHistoryErr)
		}
		if h.LastInventoryComplete.IsZero() || h.InventoryIncomplete {
			parts = append(parts, "history inventory coverage is incomplete")
		}
		if !h.HistoryStarted.IsZero() {
			parts = append(parts, fmt.Sprintf("history pass in flight for %s", now.Sub(h.HistoryStarted).Round(time.Second)))
		}
	}
	if h.LastScan.IsZero() {
		parts = append(parts, "no scan has completed")
	} else {
		parts = append(parts, fmt.Sprintf("last scan completed %s ago", now.Sub(h.LastScan).Round(time.Second)))
	}
	return strings.Join(parts, "; ")
}

// LastPoll reports when the collector last completed a successful poll pass. The
// monitor alarms on staleness: a safety system whose failure mode is
// silence would recreate the incident's invisibility inside the
// monitoring itself.
func (c *Collector) LastPoll() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.health.LastPoll
}

// UsesSocketHistory reports whether this collector polls Claudia's usage socket.
func (c *Collector) UsesSocketHistory() bool {
	return c != nil && c.usageSource != nil
}

// BindSendReceiptQuery installs this collector's replica receipt read.
// startCostGuard is the production caller. A nil collector, a nil replica,
// or a nil setter leaves the query unset. This does not start Run.
func (c *Collector) BindSendReceiptQuery(set func(func(context.Context, claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error))) {
	if c == nil || c.replica == nil || set == nil {
		return
	}
	set(c.ReplicatedReceipt)
}

// Health reports the collector's loop state (🎯T654).
func (c *Collector) Health() CollectorHealth {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.health
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// NewCollector constructs a Collector.
func NewCollector(args *CollectorArgs) *Collector {
	c := &Collector{
		store:        args.Store,
		projectsRoot: args.ProjectsRoot,
		attribute:    args.Attribute,
		activeWindow: args.ActiveWindow,
		now:          args.Now,
		usageSource:  args.UsageSource,
		replica:      args.Replica,
	}
	if c.attribute == nil {
		c.attribute = func(string) string { return "" }
	}
	if c.activeWindow == 0 {
		c.activeWindow = DefaultActiveWindow
	}
	if c.now == nil {
		c.now = time.Now
	}
	c.health.Started = c.now()
	c.health.HistoryEnabled = c.usageSource != nil || c.replica != nil
	c.health.InventoryIncomplete = c.usageSource != nil
	c.scan = c.ScanOnce
	return c
}

// ScanOnce refreshes the active-file set: billable transcripts under the
// projects tree and the spool modified within the active window. Billable =
// Grok updates.jsonl or Claude Code <session-uuid>.jsonl — other sidecars
// (chat_history, events, …) never carry usage (🎯T117). The order is
// filepath.WalkDir's. A full sweep every FullSweepInterval reads the whole
// tree; the passes between read what changed (see FullSweepInterval for the
// one transcript they can miss, and for how long).
func (c *Collector) ScanOnce() ([]string, error) {
	c.scanMu.Lock()
	defer c.scanMu.Unlock()
	now := c.now()
	c.mu.Lock()
	c.health.ScanStarted = now
	c.mu.Unlock()
	full := c.dirs == nil || c.lastFullScan.IsZero() || now.Sub(c.lastFullScan) >= FullSweepInterval || now.Before(c.lastFullScan)
	cutoff := now.Add(-c.activeWindow)
	warmCut := now.Add(-max(WarmWindow, c.activeWindow))
	prev := c.dirs
	next := make(map[string]*scanDir, len(prev))
	var files []string
	seen := make(map[string]bool)
	spoolRoot := spool.Dir()

	// visit decides one billable file, as the old walk's callback did.
	visit := func(it *scanItem) {
		if seen[it.identity()] {
			it.warm = true // decided via another spelling; look again next pass
			return
		}
		scanFileStats.Add(1)
		fi, err := os.Lstat(it.path)
		if err != nil {
			it.warm = true // vanished or unreadable: its directory will say which
			return
		}
		it.warm = fi.ModTime().After(warmCut)
		if fi.ModTime().After(cutoff) {
			seen[it.identity()] = true
			// Preserve the path spelling used by durable tail offsets.
			// Normalizing that key would replay previously consumed rows.
			files = append(files, it.path)
		}
	}

	walk := func(root string) error {
		if strings.TrimSpace(root) == "" {
			return nil
		}
		viewRoot := ""
		if spoolRoot != "" {
			var err error
			viewRoot, err = filepath.Abs(filepath.Join(spoolRoot, ".view"))
			if err != nil {
				return err
			}
		}
		// .view is generated from the dated spool for transcript readers.
		// Counting both doubles legacy costs that have no request ID.
		isView := func(identity string) bool {
			return viewRoot != "" && (identity == viewRoot || strings.HasPrefix(identity, viewRoot+string(filepath.Separator)))
		}
		var walkDir func(path, identity string, info os.FileInfo) error
		walkDir = func(path, identity string, info os.FileInfo) error {
			if isView(identity) {
				return nil
			}
			d := prev[path]
			if full || d == nil || !sameDirState(d.info, info) {
				scanListings.Add(1)
				entries, err := os.ReadDir(path)
				d = &scanDir{info: info}
				if err != nil {
					d.info = nil // WalkDir walks what it got; so do we, and list again
				}
				for _, e := range entries {
					p := filepath.Join(path, e.Name())
					id, err := filepath.Abs(p)
					if err != nil {
						return err
					}
					if id == p {
						id = "" // the common case; do not hold the path twice
					}
					d.items = append(d.items, scanItem{path: p, abs: id, dir: e.IsDir(),
						billable: !e.IsDir() && isBillableTranscript(p), warm: true})
				}
			}
			next[path] = d
			for i := range d.items {
				it := &d.items[i]
				switch {
				case it.dir:
					scanDirStats.Add(1)
					sub, err := os.Lstat(it.path)
					if err != nil || !sub.IsDir() {
						continue // gone, or no longer a directory: WalkDir could not list it either
					}
					if err := walkDir(it.path, it.identity(), sub); err != nil {
						return err
					}
				case it.billable && it.warm:
					visit(it)
				}
			}
			return nil
		}
		info, err := os.Lstat(root)
		if err != nil {
			return nil
		}
		identity, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if isBillableTranscript(root) {
				it := scanItem{path: root, abs: identity, billable: true}
				visit(&it)
			}
			return nil
		}
		return walkDir(root, identity, info)
	}
	err := walk(c.projectsRoot)
	if err == nil {
		if serr := walk(spoolRoot); serr != nil {
			err = serr
		}
	}
	c.dirs = next
	if full && err == nil {
		c.lastFullScan = now
	}
	c.mu.Lock()
	c.active = files
	c.health.LastScan = c.now()
	if full && err == nil {
		c.health.LastFullScan = now
	}
	c.health.ScanStarted = time.Time{}
	c.health.LastScanErr = errString(err)
	c.mu.Unlock()
	return files, err
}

// PollOnce tails every active file, inserting new billable events.
// Returns the number of events inserted.
func (c *Collector) PollOnce() (int, error) {
	c.mu.Lock()
	files := append([]string(nil), c.active...)
	c.health.PollStarted = c.now()
	c.mu.Unlock()

	// A store error aborts the pass WITHOUT stamping LastPoll: a
	// collector whose every pass dies in the store is stale, and the
	// alarm's detail carries the error so the reader learns why.
	fail := func(total int, err error) (int, error) {
		c.mu.Lock()
		c.health.PollStarted = time.Time{}
		c.health.LastPollErr = errString(err)
		c.mu.Unlock()
		return total, err
	}

	total := 0
	var firstErr error
	for _, path := range files {
		offset, err := c.store.TailOffset(path)
		if err != nil {
			return fail(total, err) // store errors are fatal, not per-file
		}
		events, newOffset, err := tailFile(path, offset, c.attribute, c.now())
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // a vanished/unreadable file must not stall the rest
		}
		if len(events) > 0 {
			n, err := c.store.InsertEvents(events)
			if err != nil {
				return fail(total, err)
			}
			total += n
		}
		if newOffset != offset {
			if err := c.store.SetTailOffset(path, newOffset); err != nil {
				return fail(total, err)
			}
		}
	}
	if firstErr != nil {
		// Healthy files were still ingested, but missing observations must
		// not refresh the monitor's clock of successful accounting passes.
		return fail(total, firstErr)
	}
	c.mu.Lock()
	c.health.LastPoll = c.now()
	c.health.PollStarted = time.Time{}
	c.health.LastPollErr = errString(firstErr)
	c.mu.Unlock()
	return total, firstErr
}

// Run drives the scan and poll loops until ctx is cancelled. The two
// run on separate goroutines (🎯T654): the scan walks the whole
// sessions tree (a quarter of a million files on the development host),
// and under IO pressure one slow walk on the poll goroutine would hold
// every poll behind it — which the monitor then reports as a blind
// collector. Polls keep tailing the previous active set while a scan
// is in flight.
func (c *Collector) Run(ctx context.Context, scanEvery, pollEvery time.Duration) {
	if scanEvery == 0 {
		scanEvery = DefaultScanInterval
	}
	if pollEvery == 0 {
		pollEvery = DefaultPollInterval
	}
	if c.replica != nil || c.usageSource != nil {
		go func() {
			for ctx.Err() == nil {
				var err error
				if c.replica != nil {
					err = c.CatchUpReplicas(ctx)
				} else {
					err = c.CatchUpOnce(ctx)
				}
				if err != nil {
					slog.Warn("cost collector: history", "err", err)
				}
				timer := time.NewTimer(pollEvery)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	}
	go func() {
		// One synchronous walk at a time on this goroutine, including the
		// first. Wait after completion so a slow walk cannot build a ticker
		// backlog; give IO at least as much rest as the walk consumed.
		for ctx.Err() == nil {
			started := time.Now()
			if _, err := c.scan(); err != nil {
				slog.Warn("cost collector: scan", "err", err)
			}
			delay := max(scanEvery, time.Since(started))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	poll := time.NewTicker(pollEvery)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			if _, err := c.PollOnce(); err != nil {
				slog.Warn("cost collector: poll", "err", err)
			}
		}
	}
}

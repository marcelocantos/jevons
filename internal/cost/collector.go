// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Default cadences and windows for the collection loop.
const (
	DefaultScanInterval = 30 * time.Second
	DefaultPollInterval = 3 * time.Second
	DefaultActiveWindow = 15 * time.Minute
)

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

	mu     sync.Mutex
	active []string // JSONL paths from the last scan
	health CollectorHealth
}

// CollectorHealth is the collector's own account of its two loops, so
// the monitor's staleness alarm can say WHICH way the collector is
// blind (a poll pass wedged mid-flight, a store error every pass, a
// loop that simply never woke) instead of only that it is.
type CollectorHealth struct {
	// LastPoll / LastScan are when each pass last completed.
	LastPoll time.Time `json:"last_poll"`
	LastScan time.Time `json:"last_scan"`
	// PollStarted / ScanStarted are non-zero while a pass is in flight.
	PollStarted time.Time `json:"poll_started,omitempty"`
	ScanStarted time.Time `json:"scan_started,omitempty"`
	// LastPollErr / LastScanErr are the most recent pass errors ("" when
	// the last pass was clean).
	LastPollErr string `json:"last_poll_err,omitempty"`
	LastScanErr string `json:"last_scan_err,omitempty"`
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
	if h.LastScan.IsZero() {
		parts = append(parts, "no scan has completed")
	} else {
		parts = append(parts, fmt.Sprintf("last scan completed %s ago", now.Sub(h.LastScan).Round(time.Second)))
	}
	return strings.Join(parts, "; ")
}

// LastPoll reports when the collector last completed a poll pass. The
// monitor alarms on staleness: a safety system whose failure mode is
// silence would recreate the incident's invisibility inside the
// monitoring itself.
func (c *Collector) LastPoll() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.health.LastPoll
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
	c.scan = c.ScanOnce
	return c
}

// ScanOnce walks the projects tree and refreshes the active-file set:
// billable transcripts modified within the active window. Returns the set.
// Billable = Grok updates.jsonl or Claude Code <session-uuid>.jsonl —
// other sidecars (chat_history, events, …) never carry usage (🎯T117).
func (c *Collector) ScanOnce() ([]string, error) {
	c.mu.Lock()
	c.health.ScanStarted = c.now()
	c.mu.Unlock()
	cutoff := c.now().Add(-c.activeWindow)
	var files []string
	err := filepath.WalkDir(c.projectsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isBillableTranscript(path) {
			return nil // unreadable / non-billable entries are skipped, not fatal
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(cutoff) {
			files = append(files, path)
		}
		return nil
	})
	c.mu.Lock()
	c.active = files
	c.health.LastScan = c.now()
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
	if _, err := c.scan(); err != nil {
		slog.Warn("cost collector: initial scan", "err", err)
	}
	go func() {
		scan := time.NewTicker(scanEvery)
		defer scan.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-scan.C:
				if _, err := c.scan(); err != nil {
					slog.Warn("cost collector: scan", "err", err)
				}
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

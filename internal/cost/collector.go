// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
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

	mu       sync.Mutex
	active   []string // JSONL paths from the last scan
	lastPoll time.Time
}

// LastPoll reports when the collector last completed a poll pass. The
// monitor alarms on staleness: a safety system whose failure mode is
// silence would recreate the incident's invisibility inside the
// monitoring itself.
func (c *Collector) LastPoll() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastPoll
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
	return c
}

// ScanOnce walks the projects tree and refreshes the active-file set:
// transcripts modified within the active window. Returns the set.
func (c *Collector) ScanOnce() ([]string, error) {
	cutoff := c.now().Add(-c.activeWindow)
	var files []string
	err := filepath.WalkDir(c.projectsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil // unreadable entries are skipped, not fatal
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(cutoff) {
			files = append(files, path)
		}
		return nil
	})
	c.mu.Lock()
	c.active = files
	c.mu.Unlock()
	return files, err
}

// PollOnce tails every active file, inserting new billable events.
// Returns the number of events inserted.
func (c *Collector) PollOnce() (int, error) {
	c.mu.Lock()
	files := append([]string(nil), c.active...)
	c.mu.Unlock()

	total := 0
	var firstErr error
	for _, path := range files {
		offset, err := c.store.TailOffset(path)
		if err != nil {
			return total, err // store errors are fatal, not per-file
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
				return total, err
			}
			total += n
		}
		if newOffset != offset {
			if err := c.store.SetTailOffset(path, newOffset); err != nil {
				return total, err
			}
		}
	}
	c.mu.Lock()
	c.lastPoll = c.now()
	c.mu.Unlock()
	return total, firstErr
}

// Run drives the scan/poll loop until ctx is cancelled.
func (c *Collector) Run(ctx context.Context, scanEvery, pollEvery time.Duration) {
	if scanEvery == 0 {
		scanEvery = DefaultScanInterval
	}
	if pollEvery == 0 {
		pollEvery = DefaultPollInterval
	}
	if _, err := c.ScanOnce(); err != nil {
		slog.Warn("cost collector: initial scan", "err", err)
	}
	scan := time.NewTicker(scanEvery)
	poll := time.NewTicker(pollEvery)
	defer scan.Stop()
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-scan.C:
			if _, err := c.ScanOnce(); err != nil {
				slog.Warn("cost collector: scan", "err", err)
			}
		case <-poll.C:
			if _, err := c.PollOnce(); err != nil {
				slog.Warn("cost collector: poll", "err", err)
			}
		}
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// DefaultWatchInterval is how often the Watcher stats its files. The 🎯T574
// acceptance bar is "new values take effect within 5 s"; two seconds leaves
// room for the load itself and for a poll that lands mid-write.
const DefaultWatchInterval = 2 * time.Second

// Watcher is the one loader seam for owner-editable policy files (🎯T574):
// every file that governs behaviour rather than state is registered here
// once, and re-read whenever its mtime or size changes. No policy file is
// read once at startup and then forgotten.
//
// It polls rather than subscribing to fsnotify on purpose: editors write by
// rename, sync tools rewrite in place, and a missing file is a legitimate
// "use defaults" state — an mtime poll handles all three with one code path
// and no inotify/kqueue watch to lose across a rename.
//
// A file that fails to parse never replaces the last-good value; the failure
// is logged with the path once per bad revision, not once per poll.
type Watcher struct {
	interval time.Duration
	now      func() time.Time
	mu       sync.Mutex
	files    []*watched
}

// WatcherArgs configures a Watcher. Every field is optional.
type WatcherArgs struct {
	// Interval between polls; zero uses DefaultWatchInterval.
	Interval time.Duration
}

type watched struct {
	path    string
	mtime   time.Time
	size    int64
	exists  bool
	primed  bool
	badRev  bool
	reload  func(path string) error
	changed func()
}

// NewWatcher constructs a Watcher. Call Run to start polling.
func NewWatcher(args *WatcherArgs) *Watcher {
	w := &Watcher{interval: DefaultWatchInterval, now: time.Now}
	if args != nil && args.Interval > 0 {
		w.interval = args.Interval
	}
	return w
}

// Run polls until ctx is done.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Poll()
		}
	}
}

// Poll performs one pass over every registered file, reloading those whose
// mtime or size moved since the last pass. Tests call it directly.
func (w *Watcher) Poll() {
	w.mu.Lock()
	files := append([]*watched(nil), w.files...)
	w.mu.Unlock()
	for _, f := range files {
		w.poll(f)
	}
}

func (w *Watcher) poll(f *watched) {
	st, err := os.Stat(f.path)
	exists := err == nil
	var mtime time.Time
	var size int64
	if exists {
		mtime, size = st.ModTime(), st.Size()
	}
	if f.primed && exists == f.exists && mtime.Equal(f.mtime) && size == f.size {
		return
	}
	f.primed, f.exists, f.mtime, f.size = true, exists, mtime, size
	if err := f.reload(f.path); err != nil {
		// Last-good stands. The warning names the path so the owner can find
		// the typo; it fires once per bad revision, not once per poll.
		if !f.badRev {
			slog.Warn("config watch: file unreadable — keeping last-good policy",
				"path", f.path, "err", err)
		}
		f.badRev = true
		return
	}
	if f.badRev {
		slog.Info("config watch: file readable again", "path", f.path)
	}
	f.badRev = false
	if f.changed != nil {
		f.changed()
	}
}

// Hot is a policy value kept current by a Watcher. Get is safe from any
// goroutine and always returns the last value that parsed.
type Hot[T any] struct {
	mu  sync.RWMutex
	cur T
}

// Get returns the current value.
func (h *Hot[T]) Get() T {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cur
}

// set replaces the value.
func (h *Hot[T]) set(v T) {
	h.mu.Lock()
	h.cur = v
	h.mu.Unlock()
}

// WatchArgs registers one policy file with a Watcher.
type WatchArgs[T any] struct {
	// Path is the file to watch. Required.
	Path string
	// Load parses path into a value. It is called at registration and on
	// every change. A missing file is the loader's own business (the jevons
	// loaders return defaults for it); an error keeps the previous value.
	Load func(path string) (T, error)
	// Fallback is installed when the very first Load fails, so the daemon
	// boots on defaults rather than on a zero value. Optional.
	Fallback T
	// OnChange runs after every successful load that replaced the value,
	// including the first, so consumers that copy rather than call Get can
	// re-derive. Optional.
	OnChange func(T)
}

// Watch registers a file and returns its Hot handle. The first load happens
// synchronously so the caller has a value before it wires consumers; the
// returned error is that first load's, for the caller to log. Later loads
// are the Watcher's.
func Watch[T any](w *Watcher, args *WatchArgs[T]) (*Hot[T], error) {
	h := &Hot[T]{}
	f := &watched{path: args.Path}
	f.reload = func(path string) error {
		v, err := args.Load(path)
		if err != nil {
			return err
		}
		h.set(v)
		return nil
	}
	if args.OnChange != nil {
		f.changed = func() { args.OnChange(h.Get()) }
	}
	// Prime by hand rather than through poll so the first failure is the
	// caller's to report (it decides the log level and the fallback).
	st, statErr := os.Stat(args.Path)
	f.primed, f.exists = true, statErr == nil
	if f.exists {
		f.mtime, f.size = st.ModTime(), st.Size()
	}
	first := f.reload(args.Path)
	if first != nil {
		h.set(args.Fallback)
		f.badRev = true
	} else if f.changed != nil {
		f.changed()
	}
	w.mu.Lock()
	w.files = append(w.files, f)
	w.mu.Unlock()
	return h, first
}

// RestartOnlyDiff names the config.yaml fields that changed between old and
// next but only take effect at boot — identity, listen address, and state
// paths. The caller logs them so an edit is never silently ignored; the
// hot fields (portfolios, providers, automations) are applied by the
// caller's OnChange and are not listed here.
func RestartOnlyDiff(old, next Config) []string {
	var fields []string
	type pair struct {
		name     string
		old, new string
	}
	for _, p := range []pair{
		{"owner_name", old.OwnerName, next.OwnerName},
		{"overseer_name", old.OverseerName, next.OverseerName},
		{"bind_addr", old.BindAddr, next.BindAddr},
		{"port", fmt.Sprint(old.Port), fmt.Sprint(next.Port)},
		{"workdir", old.WorkDir, next.WorkDir},
		{"provider", old.Provider, next.Provider},
		{"model", old.Model, next.Model},
		{"overseer_model", old.OverseerModel, next.OverseerModel},
		{"state_dir", old.StateDir, next.StateDir},
		{"sessions_dir", old.SessionsDir, next.SessionsDir},
		{"claude_projects", old.ClaudeProjects, next.ClaudeProjects},
		{"repos_root", old.ReposRoot, next.ReposRoot},
		{"mcp_server_name", old.MCPServerName, next.MCPServerName},
		{"persona_file", old.PersonaFile, next.PersonaFile},
	} {
		if p.old != p.new {
			fields = append(fields, p.name)
		}
	}
	return fields
}

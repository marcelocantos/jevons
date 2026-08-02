// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchAgentsFile watches agents.json (or its directory) and pushes
// agents_changed to chat listeners when the registry file mutates.
// Primary fleet refresh path is event-driven (🎯T82); clients may keep a
// slow poll only as reconnect/fallback.
//
// path is the agents registry file path (e.g. stateDir/agents.json).
// Returns a stop function; multiple calls replace the previous watcher.
func (s *Server) WatchAgentsFile(path string) (stop func()) {
	s.mu.Lock()
	if s.agentsWatchCancel != nil {
		s.agentsWatchCancel()
		s.agentsWatchCancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.agentsWatchCancel = cancel
	s.mu.Unlock()

	go s.runAgentsWatch(ctx, path)
	return cancel
}

func (s *Server) runAgentsWatch(ctx context.Context, path string) {
	if path == "" {
		return
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("agents watch: fsnotify unavailable", "err", err)
		return
	}
	defer w.Close()

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if err := w.Add(dir); err != nil {
		slog.Warn("agents watch: add dir failed", "dir", dir, "err", err)
		return
	}
	slog.Info("agents watch started", "path", path)

	// Debounce bursty atomic renames (write temp + rename).
	var (
		timer *time.Timer
		mu    = make(chan struct{}, 1)
	)
	fire := func() {
		select {
		case mu <- struct{}{}:
		default:
			return
		}
		s.NotifyAgentsChanged()
		<-mu
	}
	schedule := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(50*time.Millisecond, fire)
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Warn("agents watch error", "err", err)
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != base {
				continue
			}
			if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Rename) || ev.Has(fsnotify.Remove) {
				schedule()
			}
		}
	}
}

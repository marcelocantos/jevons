// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package eventlog is a durable append-only JSONL journal for product
// decision and lifecycle events (browser + server). Files live under
// state_dir/logs/ so the overseer and tools can read them without a
// special privilege model (🎯T120 logging initiative).
package eventlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event is one structured journal line.
type Event struct {
	TS        string         `json:"ts"` // RFC3339Nano UTC
	Source    string         `json:"source"` // browser | server
	Level     string         `json:"level"`
	Msg       string         `json:"msg"`
	Component string         `json:"component,omitempty"`
	Decision  string         `json:"decision,omitempty"`
	Corr      string         `json:"corr,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Journal is a concurrent-safe append-only log.
type Journal struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// Open creates or appends the journal at path (parents created).
func Open(path string) (*Journal, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("eventlog: path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("eventlog: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("eventlog: open: %w", err)
	}
	return &Journal{f: f, path: path}, nil
}

// Path returns the journal file path.
func (j *Journal) Path() string {
	if j == nil {
		return ""
	}
	return j.path
}

// DefaultPath returns stateDir/logs/events.jsonl.
func DefaultPath(stateDir string) string {
	return filepath.Join(stateDir, "logs", "events.jsonl")
}

// Close closes the underlying file.
func (j *Journal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return nil
	}
	err := j.f.Close()
	j.f = nil
	return err
}

// Append writes one event as a JSONL line and fsyncs.
// Best-effort callers may ignore errors; load-bearing paths should not.
func (j *Journal) Append(ev Event) error {
	if j == nil || j.f == nil {
		return fmt.Errorf("eventlog: nil journal")
	}
	if ev.TS == "" {
		ev.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if ev.Level == "" {
		ev.Level = "info"
	}
	if ev.Source == "" {
		ev.Source = "server"
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("eventlog: marshal: %w", err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("eventlog: write: %w", err)
	}
	if err := j.f.Sync(); err != nil {
		return fmt.Errorf("eventlog: fsync: %w", err)
	}
	return nil
}

// TailOptions filters a reverse-chronological tail.
type TailOptions struct {
	// Limit max events to return (default 100, max 2000).
	Limit int
	// Component exact match if non-empty.
	Component string
	// Decision exact match if non-empty.
	Decision string
	// Source exact match if non-empty (browser|server).
	Source string
	// Contains substring match on msg (case-insensitive) if non-empty.
	Contains string
}

// Tail reads the journal and returns the last matching events newest-first.
// Safe when the file is missing (returns empty).
func Tail(path string, opt TailOptions) ([]Event, error) {
	limit := opt.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 2000 {
		limit = 2000
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("eventlog: open for tail: %w", err)
	}
	defer f.Close()

	// For desk-scale files, scan all lines; keep a ring of matches.
	ring := make([]Event, 0, limit)
	sc := bufio.NewScanner(f)
	// 1 MiB lines max (decisions are small; guard against corruption).
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	comp := strings.TrimSpace(opt.Component)
	dec := strings.TrimSpace(opt.Decision)
	src := strings.TrimSpace(opt.Source)
	sub := strings.ToLower(strings.TrimSpace(opt.Contains))

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip corrupt lines
		}
		if comp != "" && ev.Component != comp {
			continue
		}
		if dec != "" && ev.Decision != dec {
			continue
		}
		if src != "" && ev.Source != src {
			continue
		}
		if sub != "" && !strings.Contains(strings.ToLower(ev.Msg), sub) {
			continue
		}
		ring = append(ring, ev)
		if len(ring) > limit {
			ring = ring[len(ring)-limit:]
		}
	}
	if err := sc.Err(); err != nil {
		return ring, fmt.Errorf("eventlog: scan: %w", err)
	}
	// Newest first for operator convenience.
	for i, j := 0, len(ring)-1; i < j; i, j = i+1, j-1 {
		ring[i], ring[j] = ring[j], ring[i]
	}
	return ring, nil
}

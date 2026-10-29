// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package eventlog is a durable append-only JSONL journal for product
// decision and lifecycle events (browser + server). Files live under
// state_dir/logs/ so the overseer and tools can read them without a
// special privilege model (🎯T120 logging initiative).
package eventlog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event is one structured journal line.
type Event struct {
	TS        string         `json:"ts"`     // RFC3339Nano UTC
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
	mu       sync.Mutex
	f        *os.File
	path     string
	writeErr error
	// Rotation (🎯T1131): when the live file reaches maxSegmentBytes it
	// becomes .1 and a fresh live file is opened; up to keepSegments files
	// are retained (live plus .1 …).
	maxSegmentBytes int64
	keepSegments    int
}

// Open creates or appends the journal at path (parents created). A nonempty
// file must end with a newline, so the next event cannot join a torn record.
// This checks the append boundary only, not the validity of earlier records.
func Open(path string) (*Journal, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("eventlog: path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("eventlog: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("eventlog: open: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("eventlog: stat append boundary: %w", err)
	}
	if info.Size() > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], info.Size()-1); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("eventlog: read append boundary: %w", err)
		}
		if last[0] != '\n' {
			_ = f.Close()
			return nil, fmt.Errorf("eventlog: unterminated journal tail; existing bytes preserved")
		}
	}
	j := &Journal{
		f:               f,
		path:            path,
		maxSegmentBytes: DefaultMaxSegmentBytes,
		keepSegments:    DefaultKeepSegments,
	}
	if info.Size() >= j.maxSegmentBytes {
		j.mu.Lock()
		err := j.rotateLocked()
		j.mu.Unlock()
		if err != nil {
			_ = j.Close()
			return nil, err
		}
	}
	return j, nil
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
	if j == nil {
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
	// Include the newline in Scanner's existing maximum buffer size. Refuse
	// before writing rather than acknowledge evidence the readers cannot see.
	if len(b)+1 > maxEventLineBytes {
		return fmt.Errorf("eventlog: encoded event exceeds reader limit of %d bytes", maxEventLineBytes)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return fmt.Errorf("eventlog: closed journal")
	}
	if j.writeErr != nil {
		return fmt.Errorf("eventlog: previous append failed; journal must be reopened: %w", j.writeErr)
	}
	line := append(b, '\n')
	n, err := j.f.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		j.writeErr = fmt.Errorf("eventlog: write: %w", err)
		return j.writeErr
	}
	if err := j.f.Sync(); err != nil {
		j.writeErr = fmt.Errorf("eventlog: fsync: %w", err)
		return j.writeErr
	}
	if err := j.maybeRotate(); err != nil {
		j.writeErr = err
		return err
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
// Safe when the file is missing (returns empty). Walks rotated segments
// newest-first (🎯T1131).
func Tail(path string, opt TailOptions) ([]Event, error) {
	if _, err := os.Stat(path); err != nil && os.IsNotExist(err) {
		return nil, nil
	}
	ev, err := Collect(path, opt)
	if err != nil {
		return nil, err
	}
	if len(ev) == 0 {
		return nil, nil
	}
	return ev, nil
}

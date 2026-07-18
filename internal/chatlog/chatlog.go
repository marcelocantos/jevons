// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package chatlog is the jevons-owned, append-only conversation record
// (🎯T30.1). The provider's private session store gives the model its
// memory; this log is what makes the conversation durable to *jevons*:
// every line broadcast to chat clients is appended and fsynced here, and
// reconnecting clients replay from it — so a dead provider process, a
// daemon restart, or a lost provider store can no longer blank the UI.
// "No conversation is ever lost."
package chatlog

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Log is a durable append-only JSONL event journal. Appends are fsynced
// before returning — at human conversation rates durability beats
// throughput. Safe for concurrent use.
type Log struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// Open opens (creating if needed) the log at path, making parent
// directories as required.
func Open(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("chatlog: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("chatlog: open: %w", err)
	}
	return &Log{f: f, path: path}, nil
}

// Path returns the log's file path.
func (l *Log) Path() string { return l.path }

// Append writes one JSONL line (newline added) and fsyncs. An append
// that cannot be made durable is an error the caller must see — never
// silently dropped.
func (l *Log) Append(line string) error {
	if line == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("chatlog: append: %w", err)
	}
	if err := l.f.Sync(); err != nil {
		return fmt.Errorf("chatlog: fsync: %w", err)
	}
	return nil
}

// Replay streams every complete line to fn in order. A final partial
// line (a crash mid-append) is skipped — prior completed turns are the
// durable record. fn returning an error stops the replay and is
// returned to the caller.
func (l *Log) Replay(fn func(line string) error) error {
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("chatlog: replay open: %w", err)
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			// No trailing newline — a torn final append; skip it.
			return nil
		}
		if err != nil {
			return fmt.Errorf("chatlog: replay read: %w", err)
		}
		line = line[:len(line)-1]
		if line == "" {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
}

// Close closes the underlying file.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

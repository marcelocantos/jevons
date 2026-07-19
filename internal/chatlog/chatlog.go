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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// TruncateTurns removes the last n user turns (and everything after
// their start) from the journal — the rewind primitive (🎯T52). A turn
// starts at a user line that does not directly follow another user line
// (the wire carries duplicate user echoes). Atomic rewrite; the append
// handle is reopened on the new file.
func (l *Log) TruncateTurns(n int) error {
	if n < 1 {
		return fmt.Errorf("chatlog: truncate turns must be >= 1")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	var lines []string
	var starts []int
	prevUser := false
	f, err := os.Open(l.path)
	if err != nil {
		return fmt.Errorf("chatlog: truncate open: %w", err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var d struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(line), &d)
		isUser := d.Type == "user"
		if isUser && !prevUser {
			starts = append(starts, len(lines))
		}
		prevUser = isUser
		lines = append(lines, line)
	}
	f.Close()
	if err := sc.Err(); err != nil {
		return fmt.Errorf("chatlog: truncate scan: %w", err)
	}
	if n > len(starts) {
		return fmt.Errorf("chatlog: only %d turns recorded, cannot rewind %d", len(starts), n)
	}
	cut := starts[len(starts)-n]

	tmp := l.path + ".rewind.tmp"
	var b strings.Builder
	for _, line := range lines[:cut] {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("chatlog: truncate write: %w", err)
	}
	if err := l.f.Close(); err != nil {
		return fmt.Errorf("chatlog: truncate close: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return fmt.Errorf("chatlog: truncate rename: %w", err)
	}
	nf, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("chatlog: truncate reopen: %w", err)
	}
	l.f = nf
	return nil
}

// Close closes the underlying file.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

// Recap distills the tail of the journal into a plain-text conversation
// summary for re-seeding a rotated overseer session (the Grok CLI only
// attaches MCP tools on session/new, so the daemon mints a fresh session
// each boot and hands the model this recap for continuity). It coalesces
// streamed assistant chunks, drops duplicate user echoes, and returns at
// most maxTurns of the newest turns within maxBytes.
func (l *Log) Recap(maxTurns, maxBytes int) string {
	type turn struct {
		role string
		text string
	}
	var turns []turn
	_ = l.Replay(func(line string) error {
		var d struct {
			Type    string `json:"type"`
			Message struct {
				StopReason string          `json:"stop_reason"`
				Content    json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &d) != nil {
			return nil
		}
		var text string
		var s string
		if json.Unmarshal(d.Message.Content, &s) == nil {
			text = s
		} else {
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(d.Message.Content, &parts) == nil {
				for _, p := range parts {
					text += p.Text
				}
			}
		}
		switch d.Type {
		case "user":
			if text != "" && (len(turns) == 0 || turns[len(turns)-1].role != "user" || turns[len(turns)-1].text != text) {
				turns = append(turns, turn{"user", text})
			}
		case "assistant":
			if text != "" {
				if len(turns) > 0 && turns[len(turns)-1].role == "assistant" {
					turns[len(turns)-1].text += text
				} else {
					turns = append(turns, turn{"assistant", text})
				}
			}
		}
		return nil
	})
	if len(turns) > maxTurns {
		turns = turns[len(turns)-maxTurns:]
	}
	var b strings.Builder
	for _, t := range turns {
		fmt.Fprintf(&b, "%s: %s\n", t.role, t.text)
	}
	out := b.String()
	if len(out) > maxBytes {
		out = out[len(out)-maxBytes:]
	}
	return out
}

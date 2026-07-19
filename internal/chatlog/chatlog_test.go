// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package chatlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chatlog", "jevons.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	lines := []string{`{"type":"user","text":"hi"}`, `{"type":"assistant","text":"hello"}`}
	for _, ln := range lines {
		if err := l.Append(ln); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Restart: a fresh Log over the same path replays every turn.
	l2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer l2.Close()
	var got []string
	if err := l2.Replay(func(line string) error { got = append(got, line); return nil }); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(got) != 2 || got[0] != lines[0] || got[1] != lines[1] {
		t.Fatalf("Replay = %v, want %v", got, lines)
	}
}

func TestReplaySkipsTornFinalLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.jsonl")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"torn\":"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()
	var got []string
	if err := l.Replay(func(line string) error { got = append(got, line); return nil }); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(got) != 1 || got[0] != `{"a":1}` {
		t.Fatalf("Replay = %v, want just the complete line", got)
	}
}

func TestReplayMissingFileIsEmpty(t *testing.T) {
	l, err := Open(filepath.Join(t.TempDir(), "fresh.jsonl"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()
	os.Remove(l.Path())
	if err := l.Replay(func(string) error { t.Fatal("no lines expected"); return nil }); err != nil {
		t.Fatalf("Replay: %v", err)
	}
}

// 🎯T52: rewind's journal primitive. Duplicate user echoes must count
// as one turn boundary; the append handle must survive the rewrite.
func TestTruncateTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	lines := []string{
		`{"type":"user","message":{"content":"turn one"}}`,
		`{"type":"user","message":{"content":"turn one"}}`, // duplicate echo
		`{"type":"assistant","message":{"content":[{"type":"text","text":"a1"}]}}`,
		`{"type":"user","message":{"content":"turn two"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"a2"}]}}`,
	}
	for _, ln := range lines {
		if err := l.Append(ln); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.TruncateTurns(1); err != nil {
		t.Fatalf("TruncateTurns: %v", err)
	}
	var got []string
	if err := l.Replay(func(line string) error { got = append(got, line); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !strings.Contains(got[2], "a1") {
		t.Fatalf("after rewind 1: %v", got)
	}
	// Append still works post-rewrite (handle reopened).
	if err := l.Append(`{"type":"user","message":{"content":"turn three"}}`); err != nil {
		t.Fatalf("append after truncate: %v", err)
	}
	if err := l.TruncateTurns(5); err == nil {
		t.Fatal("over-rewind must error")
	}
}

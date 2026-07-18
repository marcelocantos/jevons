// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package chatlog

import (
	"os"
	"path/filepath"
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

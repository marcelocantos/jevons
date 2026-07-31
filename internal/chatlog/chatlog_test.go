// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package chatlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealLinesCoalescesTokenStream(t *testing.T) {
	raw := []string{
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hel"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"lo"}]}}`,
		`{"type":"assistant","message":{"content":[],"stop_reason":"end_turn"}}`,
		`{"type":"user","message":{"content":"bye"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}}`,
	}
	got := SealLines(raw)
	if len(got) != 4 {
		t.Fatalf("sealed %d lines %v, want 4 (user, asst, user, asst)", len(got), got)
	}
	if !strings.Contains(got[1], "Hello") {
		t.Fatalf("first assistant not coalesced: %s", got[1])
	}
	if !strings.Contains(got[1], "end_turn") {
		t.Fatalf("first assistant missing stop: %s", got[1])
	}
	if !strings.Contains(got[3], "ok") {
		t.Fatalf("second assistant: %s", got[3])
	}
}

func TestSealLinesDropsToolOnlyMidFrames(t *testing.T) {
	raw := []string{
		`{"type":"user","message":{"content":"x"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}}`,
	}
	got := SealLines(raw)
	if len(got) != 2 {
		t.Fatalf("got %d %v, want user+sealed asst", len(got), got)
	}
	if strings.Contains(got[1], "tool_use") {
		t.Fatalf("tool frame leaked into sealed: %s", got[1])
	}
}

func TestReplayTailSeals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, ln := range []string{
		`{"type":"user","message":{"content":"a"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"1"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"2"}]}}`,
		`{"type":"assistant","message":{"content":[],"stop_reason":"end_turn"}}`,
	} {
		if err := l.Append(ln); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	_, _, err = l.ReplayTail(30, func(line string) error { got = append(got, line); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("replay sealed %d frames, want 2: %v", len(got), got)
	}
	if !strings.Contains(got[1], "12") {
		t.Fatalf("coalesced text missing: %s", got[1])
	}
}

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

// 🎯T57: capped replay + range read for "load earlier".
func TestReplayTailAndReadRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	// 4 turns: each a user line + an assistant line.
	for i := 0; i < 4; i++ {
		if err := l.Append(`{"type":"user","message":{"content":"q` + itoa(i) + `"}}`); err != nil {
			t.Fatal(err)
		}
		if err := l.Append(`{"type":"assistant","message":{"content":[{"type":"text","text":"a` + itoa(i) + `"}]}}`); err != nil {
			t.Fatal(err)
		}
	}
	// Tail of the last 2 turns → lines for turns 2,3 (indices 4..7); start=4, total=8.
	var got []string
	start, total, err := l.ReplayTail(2, func(line string) error { got = append(got, line); return nil })
	if err != nil {
		t.Fatalf("ReplayTail: %v", err)
	}
	if total != 8 || start != 4 || len(got) != 4 {
		t.Fatalf("ReplayTail: start=%d total=%d lines=%d, want 4/8/4", start, total, len(got))
	}
	if !strings.Contains(got[0], "q2") {
		t.Fatalf("tail did not start at turn 2: %q", got[0])
	}
	// maxTurns >= turns → whole log.
	got = nil
	start, _, _ = l.ReplayTail(99, func(line string) error { got = append(got, line); return nil })
	if start != 0 || len(got) != 8 {
		t.Fatalf("ReplayTail(all): start=%d lines=%d, want 0/8", start, len(got))
	}
	// ReadRange for the older window before the tail (lines [0,4)).
	older, total2, err := l.ReadRange(0, 4)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if total2 != 8 || len(older) != 4 || !strings.Contains(older[0], "q0") {
		t.Fatalf("ReadRange(0,4): total=%d n=%d", total2, len(older))
	}
	if out, _, _ := l.ReadRange(6, 3); out != nil {
		t.Fatalf("reversed range should be empty, got %v", out)
	}
}

func itoa(i int) string { return string(rune('0' + i)) }

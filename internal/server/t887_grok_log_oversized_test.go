// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T887: one oversized line in the Grok conversation JSONL must not blank
// the whole Tail read. Before this fix, Tail used bufio.Scanner with a
// 1 MiB buffer cap; the first line over that ceiling (e.g. a worker's
// completion note pasting back megabytes of tool output) failed the whole
// scan with "bufio.Scanner: token too long" and Tail returned an error
// instead of the entries around it.
func TestGrokLogTailSkipsOversizedLineInsteadOfAborting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	g, err := NewGrokLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Append(GrokLogEntry{Role: "user", Content: "before"}); err != nil {
		t.Fatal(err)
	}
	// A single line well over the old 1 MiB bufio.Scanner cap.
	huge := strings.Repeat("x", 2<<20)
	if err := g.Append(GrokLogEntry{Role: "assistant", Content: huge}); err != nil {
		t.Fatal(err)
	}
	if err := g.Append(GrokLogEntry{Role: "user", Content: "after"}); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := g.Tail(10)
	if err != nil {
		t.Fatalf("Tail returned an error instead of skipping the oversized line: %v", err)
	}
	var roles []string
	for _, e := range entries {
		roles = append(roles, e.Role+":"+e.Content)
	}
	if len(entries) != 3 {
		t.Fatalf("entries=%d %v, want 3 (the huge line is still a valid JSONL record within the hard cap)", len(entries), roles)
	}
	if entries[0].Content != "before" || entries[2].Content != "after" {
		t.Fatalf("entries=%v, want before/huge/after in order", roles)
	}
	if entries[1].Content != huge {
		t.Fatal("the oversized-but-under-hard-cap line was dropped instead of read")
	}
}

// A line over the hard cap (not just the old scanner cap) is skipped, and
// the surrounding entries still come back — the read never aborts.
func TestGrokLogTailSkipsLineOverHardCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	g, err := NewGrokLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Append(GrokLogEntry{Role: "user", Content: "before"}); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	// Hand-write a line over grokLogHardLineCap directly: Append's own
	// json.Marshal round-trip is not the thing under test here.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	huge := `{"timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","role":"assistant","content":"` +
		strings.Repeat("y", grokLogHardLineCap+1024) + `"}`
	if _, err := f.WriteString(huge + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	g2, err := NewGrokLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := g2.Append(GrokLogEntry{Role: "user", Content: "after"}); err != nil {
		t.Fatal(err)
	}
	if err := g2.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := g2.Tail(10)
	if err != nil {
		t.Fatalf("Tail returned an error instead of skipping the over-hard-cap line: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%d, want 2 (before, after) with the over-hard-cap line dropped", len(entries))
	}
	if entries[0].Content != "before" || entries[1].Content != "after" {
		t.Fatalf("entries=%+v, want before/after", entries)
	}
}

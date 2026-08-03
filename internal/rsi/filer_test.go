// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"strings"
	"testing"
)

func TestBullseyeFilerArgs(t *testing.T) {
	var saw []string
	f := BullseyeFiler{
		Run: func(args ...string) (string, error) {
			saw = append([]string{}, args...)
			return "ok\nids: T155\n", nil
		},
	}
	id, err := f.File(FileArgs{
		Cwd:        t.TempDir(),
		Name:       "MCP timeouts are bounded",
		Acceptance: []string{"No unbounded wait"},
		Context:    "from ambient RSI",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "T155" {
		t.Fatalf("id=%q", id)
	}
	joined := strings.Join(saw, " ")
	for _, want := range []string{"commit", "track", "MCP timeouts", "No unbounded wait", "ambient-rsi"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, saw)
		}
	}
}

// 🎯T226: RSI filer always tracks — never attaches to an existing open leaf.
func TestBullseyeFilerAlwaysAllocatesNewID(t *testing.T) {
	trackCalls := 0
	f := BullseyeFiler{
		Run: func(args ...string) (string, error) {
			trackCalls++
			return "ok\nids: T999\n", nil
		},
	}
	id, err := f.File(FileArgs{
		Cwd:        t.TempDir(),
		Name:       "Inspect user MD injects for fleet",
		Acceptance: []string{"user injects render as MD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "T999" {
		t.Fatalf("id=%q want T999 (new allocation)", id)
	}
	if trackCalls != 1 {
		t.Fatalf("trackCalls=%d want 1", trackCalls)
	}
}

func TestParseBullseyeTrackID(t *testing.T) {
	if got := parseBullseyeTrackID("ids: T12.3\n"); got != "T12.3" {
		t.Fatalf("got %q", got)
	}
}

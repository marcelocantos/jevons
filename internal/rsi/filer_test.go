// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/targetfile"
)

func TestBullseyeFilerArgs(t *testing.T) {
	var saw []string
	f := BullseyeFiler{
		Run: func(args ...string) (string, error) {
			saw = append([]string{}, args...)
			return "ok\nids: T155\n", nil
		},
		// Empty open set so unique names track normally.
		LoadOpen: func(cwd string) ([]targetfile.OpenLeaf, error) { return nil, nil },
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

// 🎯T222: RSI filer attaches to existing open near-dup instead of track.
func TestBullseyeFilerAttachesNearDuplicate(t *testing.T) {
	trackCalls := 0
	f := BullseyeFiler{
		Run: func(args ...string) (string, error) {
			trackCalls++
			return "ok\nids: T999\n", nil
		},
		LoadOpen: func(cwd string) ([]targetfile.OpenLeaf, error) {
			return []targetfile.OpenLeaf{{
				ID: "T220", Name: "Inspect user MD injects", Status: "identified",
				Acceptance: []string{"user injects render as MD"},
			}}, nil
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
	if id != "T220" {
		t.Fatalf("id=%q want T220", id)
	}
	if trackCalls != 0 {
		t.Fatalf("trackCalls=%d want 0", trackCalls)
	}
}

func TestParseBullseyeTrackID(t *testing.T) {
	if got := parseBullseyeTrackID("ids: T12.3\n"); got != "T12.3" {
		t.Fatalf("got %q", got)
	}
}

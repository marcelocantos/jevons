// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package docratchet holds hermetic prose/inventory ratchets.
// Not user journeys — see README.md and 🎯T107.
package docratchet_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readRepo(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// TestJourneyDoctrineMarkers is a *prose ratchet*: it fails if standing
// product sentences are deleted from docs. It does not exercise product
// runtime behaviour (no agent, no jevonsd).
func TestJourneyDoctrineMarkers(t *testing.T) {
	agents := readRepo(t, "AGENTS.md")
	readme := readRepo(t, "scripts/journey-suite/README.md")
	for _, doc := range []struct {
		name, body string
		need       []string
	}{
		{"AGENTS.md", agents, []string{
			"preferred E2E net",
			"distinct from",
			"make test",
			"Journey-or-exception",
			"13705",
			"test-journey",
			"must interact with an agent", // T107 definition
			"alter ego",                 // T98 CEO identity link
			"ceo-alter-ego.md",
			"T98",
		}},
		{"journey README", readme, []string{
			"preferred E2E net",
			"distinct from",
			"make test",
			"Journey-or-exception",
			"must interact with an agent",
			"13705",
			"refuses",
		}},
	} {
		for _, m := range doc.need {
			if !strings.Contains(doc.body, m) {
				t.Errorf("%s missing doctrine marker %q", doc.name, m)
			}
		}
	}
}

// TestCEOAlterEgoDoctrineMarkers ratchets 🎯T98 draft doctrine: alter-ego
// north star, multi-dimension map, surface/target linkage, voice-first,
// ramifications vs chat wrapper. Owner ratification residual is explicit
// in the note; this only keeps the note from rotting.
func TestCEOAlterEgoDoctrineMarkers(t *testing.T) {
	note := readRepo(t, "docs/design/ceo-alter-ego.md")
	for _, want := range []string{
		"alter ego",
		"T98",
		"not a passive butler",
		"chat wrapper",
		"Impatience",
		"Resourcefulness",
		"Risk, escalation",
		"Quality bar",
		"Communication style",
		"Attention management",
		"Multi-agent orchestration",
		"Self-improvement",
		"Tool & permission boldness",
		"Delivery vocabulary",
		"Voice-first",
		"persona.md",
		"fleet",
		"bullseye",
		"T87",
		"T90",
		"T92",
		"T96",
		"T104",
		"T78",
		"T125",
		"T129",
		"T130",
		"Owner review",
		"no slash",
		"draft for owner review",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("ceo-alter-ego.md missing T98 marker %q", want)
		}
	}
}

// TestJourneyInventoryRegistered fails if README lists a journey that
// main.go no longer registers (or the reverse for the J1–J10 set).
func TestJourneyInventoryRegistered(t *testing.T) {
	readme := readRepo(t, "scripts/journey-suite/README.md")
	mainSrc := readRepo(t, "scripts/journey-suite/main.go")
	for _, id := range []string{
		"J1-health",
		"J2-chat-round-trip",
		"J3-cancel-and-send",
		"J4-reconnect-sealed",
		"J5-isolation",
		"J6-mcp-tool-surface",
		"J7-overseer-registry",
		"J8-two-agents-same-workdir",
		"J9-thread-spawn-direct",
		"J10-worker-shell-tool",
	} {
		if !strings.Contains(readme, id) {
			t.Errorf("README inventory missing %s", id)
		}
		if !strings.Contains(mainSrc, `"`+id+`"`) {
			t.Errorf("main.go does not register journey %q", id)
		}
	}
}

// TestJourneySuiteWiresPortguard fails if the suite stops calling the
// shared RefuseDaily helper (duplicate reimplementation risk).
func TestJourneySuiteWiresPortguard(t *testing.T) {
	mainSrc := readRepo(t, "scripts/journey-suite/main.go")
	if !strings.Contains(mainSrc, "portguard.RefuseDaily") {
		t.Fatal("main.go must call portguard.RefuseDaily (not a local reimplementation)")
	}
	if strings.Count(mainSrc, "portguard.RefuseDaily") < 2 {
		t.Fatal("want RefuseDaily at startup and isolation (at least 2 call sites)")
	}
}

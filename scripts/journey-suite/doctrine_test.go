// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 🎯T101 hermetic ratchet: journey doctrine markers + daily-port refusal
// cannot silently drift off the ledger. No live Grok.

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// scripts/journey-suite/doctrine_test.go → repo root
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

func TestT101DoctrineMarkersInDocs(t *testing.T) {
	agents := readRepo(t, "AGENTS.md")
	readme := readRepo(t, "scripts/journey-suite/README.md")

	// Preferred E2E net + distinct from hermetic make test.
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
		}},
		{"journey README", readme, []string{
			"preferred E2E net",
			"distinct from",
			"make test",
			"Journey-or-exception",
			"never",
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

func TestT101JourneyInventoryJ1ThroughJ10(t *testing.T) {
	readme := readRepo(t, "scripts/journey-suite/README.md")
	mainSrc := readRepo(t, "scripts/journey-suite/main.go")
	// Inventory in README (docs) and registration in main (code).
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
		// main registers as s.run("J…", …) — match bare id prefix in run calls
		if !strings.Contains(mainSrc, `"`+id+`"`) {
			t.Errorf("main.go does not register journey %q", id)
		}
	}
}

func TestT101SuiteRefusesDailyPort(t *testing.T) {
	// Drive the shipped refuseDailyPort function — not a source grep that
	// a comment alone could satisfy (skeptic: criterion 5).
	if dailyPort != 13705 {
		t.Fatalf("dailyPort = %d, want 13705", dailyPort)
	}
	if defaultPort == dailyPort {
		t.Fatal("defaultPort must not equal dailyPort")
	}

	err := refuseDailyPort(dailyPort)
	if err == nil {
		t.Fatal("refuseDailyPort(dailyPort) returned nil — daily driver would be allowed")
	}
	if !strings.Contains(err.Error(), "refusing port") {
		t.Fatalf("error %q missing refusing port", err)
	}
	if !strings.Contains(err.Error(), "daily-driver") {
		t.Fatalf("error %q missing daily-driver", err)
	}
	if !strings.Contains(err.Error(), "13705") {
		t.Fatalf("error %q missing port number", err)
	}

	if err := refuseDailyPort(defaultPort); err != nil {
		t.Fatalf("refuseDailyPort(defaultPort) = %v, want nil", err)
	}
	if err := refuseDailyPort(0); err != nil {
		t.Fatalf("refuseDailyPort(0) = %v, want nil (ephemeral resolved later)", err)
	}
	if err := refuseDailyPort(13716); err != nil {
		t.Fatalf("refuseDailyPort(13716) = %v, want nil", err)
	}

	// Structural: main and isolation must call refuseDailyPort (not a
	// parallel reimplementation that could drift).
	mainSrc := readRepo(t, "scripts/journey-suite/main.go")
	if strings.Count(mainSrc, "refuseDailyPort(") < 3 {
		// def + startup call + assertIsolation call
		t.Fatalf("refuseDailyPort call sites in main.go: want ≥3 (def+startup+isolation), source must wire the function")
	}
}

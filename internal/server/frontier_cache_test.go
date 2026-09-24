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

// On 2026-09-21 one GET /api/frontier took 19s on a loaded host: every ask
// spawned the bullseye CLI and re-parsed a 2.9 MB ledger, and every open
// cockpit tab asks every 8s. The panel read "0 ready" and restart-jevonsd's
// readiness probe timed out against a daemon that was serving.
func TestFrontierIsNotRecomputedForAnUnchangedLedger(t *testing.T) {
	prev := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = prev })

	dir := t.TempDir()
	ledger := filepath.Join(dir, "shadow", "bullseye.yaml")
	if err := os.MkdirAll(filepath.Dir(ledger), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string, at time.Time) {
		t.Helper()
		if err := os.WriteFile(ledger, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(ledger, at, at); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	write("targets:\n  T9:\n    name: First\n    status: identified\n", base)

	opens := 0
	runBullseyeCLI = func(args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "open") {
			opens++
			return "File: " + ledger + "\nActive: 1\n", nil
		}
		return "", nil
	}
	cwd := filepath.Join(dir, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	first := loadFrontier(cwd)
	if !first.Available || len(first.Targets) != 1 || first.Targets[0].ID != "T9" {
		t.Fatalf("first load: %+v", first)
	}

	// Unreadable, same size and mtime: an answer now can only be the cache.
	if err := os.Chmod(ledger, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(ledger, base, base); err != nil {
		t.Fatal(err)
	}
	second := loadFrontier(cwd)
	if !second.Available || len(second.Targets) != 1 {
		t.Fatalf("unchanged ledger was re-read (it is unreadable, so this failed): %+v", second)
	}
	if opens != 1 {
		t.Fatalf("bullseye open ran %d times for one cwd, want 1", opens)
	}

	// A write moves the mtime, and the very next ask must see it.
	if err := os.Chmod(ledger, 0o644); err != nil {
		t.Fatal(err)
	}
	write("targets:\n  T9:\n    name: First\n    status: identified\n  T12:\n    name: Second\n    status: identified\n", base.Add(time.Minute))
	third := loadFrontier(cwd)
	if len(third.Targets) != 2 {
		t.Fatalf("a changed ledger served the stale frontier: %+v", third.Targets)
	}

	// The ledger moving away invalidates the remembered path.
	if err := os.Remove(ledger); err != nil {
		t.Fatal(err)
	}
	_ = loadFrontier(cwd)
	if opens != 2 {
		t.Fatalf("a vanished ledger was not rediscovered: bullseye open ran %d times, want 2", opens)
	}
}

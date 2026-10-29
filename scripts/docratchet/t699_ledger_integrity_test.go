// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestT699LedgerHasNoAchievedDependingOnOpen ratchets 🎯T699: no achieved
// target may depend on an open predecessor (skipped nodes degrade frontier reads).
// Reconciliation for the 2026-09-20 T553.2/T553.1 incident: T553.1 was
// achieved 2026-09-22 (seeded-throw oracle, ceeb564e); T553.2 was re-achieved
// after that — the depends_on edge was correct, not stale.
func TestT699LedgerHasNoAchievedDependingOnOpen(t *testing.T) {
	root := repoRoot(t)
	if _, err := exec.LookPath("bullseye"); err != nil {
		t.Skip("bullseye not on PATH")
	}

	for _, view := range []string{"validate", "context"} {
		cmd := exec.Command("bullseye", "query", "--view", view, "--cwd", root)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bullseye query --view %s: %v\n%s", view, err, out)
		}
		text := string(out)
		if strings.Contains(text, "achieved while depending on open target") {
			t.Fatalf("bullseye --view %s reports achieved-while-depending:\n%s", view, text)
		}
		if strings.Contains(text, "Validation errors (degraded read)") {
			t.Fatalf("bullseye --view %s reports degraded validation section:\n%s", view, text)
		}
	}

	// Pin the reconciled T553 pair so a future reopen of T553.1 without T553.2
	// revert is caught by the oracle above; document the decision in-repo.
	for _, id := range []string{"T553.1", "T553.2"} {
		cmd := exec.Command("bullseye", "query", "--view", "target", "--id", id, "--cwd", root)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bullseye query target %s: %v\n%s", id, err, out)
		}
		if !strings.Contains(string(out), "status: achieved") {
			t.Fatalf("expected %s achieved for T699 reconciliation; got:\n%s", id, out)
		}
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestTrustRepoTesthomeCensus exercises the actual fixture constructor and
// the production census, not merely the text of the seeded blank import.
// A new test package in a trust journey must not make the integrator's
// unrelated trunk gate red before the worker's change is examined (🎯T1325).
func TestTrustRepoTesthomeCensus(t *testing.T) {
	if _, err := exec.LookPath("bullseye"); err != nil {
		t.Skip("bullseye CLI unavailable")
	}
	root, err := jevonsRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	repo, err := prepareTrustJourneyRepo(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ pkg, test string }{
		{"./internal/testhome", "TestEveryTestBinaryLinksTesthome"},
		{"./scripts/docratchet", "TestT699LedgerHasNoAchievedDependingOnOpen"},
		{"./scripts/docratchet", "TestT1090NoNewHomeJevonsJoinsInDaemonCode"},
	} {
		cmd := exec.Command("go", "test", "-count=1", "-run", "^"+tc.test+"$", tc.pkg)
		cmd.Dir = repo
		cmd.Env = append(cmd.Environ(), "GOWORK=off")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "ok  ") {
			t.Fatalf("trust repo %s: %v\n%s", tc.test, err, out)
		}
	}
}

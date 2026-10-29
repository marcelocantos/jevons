// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// Oracles for 🎯T1314: integrate accepts a repo's declared full gate
// (make gate-checks) as covering every changed package, and
// -override-gate-check bypasses a coverage refusal as well as a missing
// record.

func TestT1314RequireLandingGateAcceptsMakeGateChecks(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerGoPackagesCommit(t, base, "jv-t1314-full")
	gateDir := t.TempDir()
	savePristineGateRecord(t, gateDir, "g-gate-checks", sha, []string{"make", "gate-checks"})

	rec, err := worktree.RequireLandingGate(base, "jv-t1314-full", gateDir, "")
	if err != nil {
		t.Fatalf("RequireLandingGate: %v", err)
	}
	if rec == nil || rec.ID != "g-gate-checks" {
		t.Fatalf("expected accepted make gate-checks record, got %+v", rec)
	}
}

func TestT1314OverrideHonoursCoverageCheck(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerGoPackagesCommit(t, base, "jv-t1314-override")
	gateDir := t.TempDir()
	savePristineGateRecord(t, gateDir, "g-narrow", sha, []string{
		"go", "test", "./internal/server",
	})

	_, err := worktree.RequireLandingGate(base, "jv-t1314-override", gateDir, "")
	if err == nil || !errors.Is(err, gate.ErrLandingGateCoverage) {
		t.Fatalf("expected coverage refusal without override, got %v", err)
	}

	reason := "owner override of coverage check, 2026-10-29 claudia T237"
	rec, err := worktree.RequireLandingGate(base, "jv-t1314-override", gateDir, reason)
	if err != nil {
		t.Fatalf("expected override to bypass coverage refusal, got %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil record for an overridden coverage check, got %+v", rec)
	}

	common := git(t, base, "rev-parse", "--path-format=absolute", "--git-common-dir")
	log, err := os.ReadFile(filepath.Join(common, "jevons-integrate-overrides.log"))
	if err != nil {
		t.Fatalf("read override log: %v", err)
	}
	want := fmt.Sprintf("agent=jv-t1314-override tip=%s reason=%s", sha, reason)
	if !strings.Contains(string(log), want) {
		t.Fatalf("override log %q does not contain %q", log, want)
	}
}

func TestT1314OverrideHonoursCitedCoverageCheck(t *testing.T) {
	base := sharedClone(t)
	wt, sha := workerGoPackagesCommit(t, base, "jv-t1314-cited")
	gateDir := t.TempDir()
	savePristineGateRecord(t, gateDir, "c9d6a358", sha, []string{
		"go", "test", "./internal/server",
	})
	git(t, wt, "commit", "-q", "--allow-empty", "-m",
		"same tree with citation\n\nGATE t1314-test exit=0 GREEN id=c9d6a358 out=deadbeefcafe dur=1s")

	_, err := worktree.RequireLandingGate(base, "jv-t1314-cited", gateDir, "")
	if err == nil || !errors.Is(err, gate.ErrLandingGateCoverage) {
		t.Fatalf("expected cited coverage refusal without override, got %v", err)
	}

	rec, err := worktree.RequireLandingGate(base, "jv-t1314-cited", gateDir, "owner override cited coverage")
	if err != nil {
		t.Fatalf("expected override to bypass cited coverage refusal, got %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil record for overridden cited coverage, got %+v", rec)
	}
}

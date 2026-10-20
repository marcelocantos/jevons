// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// Oracles for 🎯T1107: cmd/integrate's landing precondition refuses a
// worker branch with no GREEN gate record naming its tip, lands one that
// has a GREEN record on its tip, and refuses one whose only record is on an
// older commit.

func saveGateRecord(t *testing.T, root, id, commit string, v gate.Verdict, started time.Time) {
	t.Helper()
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatalf("open gate store: %v", err)
	}
	rec := &gate.Record{
		ID:          id,
		Name:        "t1107-test",
		Command:     []string{"true"},
		Started:     started,
		Ended:       started.Add(time.Second),
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     v,
		Tree: &gate.TreeProvenance{
			Repo:   root,
			Commit: commit,
			Clean:  true,
		},
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("save gate record: %v", err)
	}
}

func TestRequireLandingGateRefusesWithNoRecord(t *testing.T) {
	base := sharedClone(t)
	_, _ = workerCommit(t, base, "jv-nogate", "a.txt", "nogate\n")
	gateDir := t.TempDir()

	_, err := worktree.RequireLandingGate(base, "jv-nogate", gateDir, "")
	if err == nil {
		t.Fatalf("expected refusal with no gate record, got nil")
	}
	if !errors.Is(err, worktree.ErrNoGateRecord) {
		t.Fatalf("expected ErrNoGateRecord, got %v", err)
	}
}

func TestRequireLandingGateLandsWithGreenRecordOnTip(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerCommit(t, base, "jv-greentip", "a.txt", "green\n")
	gateDir := t.TempDir()
	saveGateRecord(t, gateDir, "g1", sha, gate.VerdictGreen, time.Now())

	rec, err := worktree.RequireLandingGate(base, "jv-greentip", gateDir, "")
	if err != nil {
		t.Fatalf("RequireLandingGate: %v", err)
	}
	if rec == nil || rec.ID != "g1" {
		t.Fatalf("expected the GREEN record on the tip, got %+v", rec)
	}

	res, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-greentip"})
	if err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	mustReach(t, base, sha)
	if len(res.Landed) != 1 {
		t.Fatalf("expected 1 landed commit, got %d", len(res.Landed))
	}
}

func TestRequireLandingGateRefusesRecordOnOlderCommit(t *testing.T) {
	base := sharedClone(t)
	_, oldSHA := workerCommit(t, base, "jv-stale", "a.txt", "first\n")
	wt := worktree.WorktreePath(base, "jv-stale")
	writeFile(t, filepath.Join(wt, "a.txt"), "second\n")
	git(t, wt, "add", "a.txt")
	git(t, wt, "commit", "-q", "-m", "jv-stale second edit")

	gateDir := t.TempDir()
	saveGateRecord(t, gateDir, "g-old", oldSHA, gate.VerdictGreen, time.Now())

	_, err := worktree.RequireLandingGate(base, "jv-stale", gateDir, "")
	if err == nil {
		t.Fatalf("expected refusal: record names an older commit, not the tip")
	}
	if !errors.Is(err, worktree.ErrNoGateRecord) {
		t.Fatalf("expected ErrNoGateRecord, got %v", err)
	}
}

func TestRequireLandingGateRefusesRedRecordOnTip(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerCommit(t, base, "jv-red", "a.txt", "red\n")
	gateDir := t.TempDir()
	saveGateRecord(t, gateDir, "g-red", sha, gate.VerdictRed, time.Now())

	_, err := worktree.RequireLandingGate(base, "jv-red", gateDir, "")
	if err == nil {
		t.Fatalf("expected refusal: only record on tip is RED, not GREEN")
	}
	if !errors.Is(err, worktree.ErrNoGateRecord) {
		t.Fatalf("expected ErrNoGateRecord, got %v", err)
	}
}

func TestRequireLandingGateOverrideLogsAndSkipsCheck(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerCommit(t, base, "jv-override", "a.txt", "override\n")
	gateDir := t.TempDir()

	rec, err := worktree.RequireLandingGate(base, "jv-override", gateDir, "owner says land it, 2026-10-21")
	if err != nil {
		t.Fatalf("expected override to bypass the check, got %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil record for an overridden check, got %+v", rec)
	}

	common := git(t, base, "rev-parse", "--path-format=absolute", "--git-common-dir")
	log, err := os.ReadFile(filepath.Join(common, "jevons-integrate-overrides.log"))
	if err != nil {
		t.Fatalf("read override log: %v", err)
	}
	want := fmt.Sprintf("agent=jv-override tip=%s reason=owner says land it, 2026-10-21", sha)
	if !contains(string(log), want) {
		t.Fatalf("override log %q does not contain %q", log, want)
	}
}

func TestRequireLandingGateHoldsCitedGateIDToTip(t *testing.T) {
	base := sharedClone(t)
	wt, err := worktree.Ensure(base, "jv-cited")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "a.txt"), "cited\n")
	git(t, wt, "add", "a.txt")

	gateDir := t.TempDir()
	// The gate record measures a commit that does not exist yet: the cited
	// id is wired to a different commit than the one about to carry it, the
	// shape of a worker pasting an earlier run's id into a later commit.
	saveGateRecord(t, gateDir, "g-mismatch", "0000000000000000000000000000000000000000", gate.VerdictGreen, time.Now())
	git(t, wt, "commit", "-q", "-m", "jv-cited edits a.txt\n\nGATE t1107-test exit=0 GREEN id=g-mismatch out=deadbeefcafe dur=0.1s")

	_, err = worktree.RequireLandingGate(base, "jv-cited", gateDir, "")
	if err == nil {
		t.Fatalf("expected refusal: cited record does not measure the tip")
	}
	if !errors.Is(err, worktree.ErrCitedGateMismatch) {
		t.Fatalf("expected ErrCitedGateMismatch, got %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

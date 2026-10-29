// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// Oracles for 🎯T1146: integrate refuses a tip whose only GREEN gate was
// measured inside an inherited workspace, and refuses a committed marcelocantos
// replace in go.mod.

func saveNonPristineGreen(t *testing.T, root, id, commit string) {
	t.Helper()
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatalf("open gate store: %v", err)
	}
	rec := &gate.Record{
		ID:          id,
		Name:        "t1146-workspace",
		Command:     []string{"true"},
		Started:     time.Now(),
		Ended:       time.Now().Add(time.Second),
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     gate.VerdictGreen,
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

func TestRequireLandingGateRefusesWorkspaceGreenWithoutPristine(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerCommit(t, base, "jv-workspace", "a.txt", "workspace gate\n")
	gateDir := t.TempDir()
	saveNonPristineGreen(t, gateDir, "g-ws", sha)

	_, err := worktree.RequireLandingGate(base, "jv-workspace", gateDir, "")
	if err == nil {
		t.Fatal("expected refusal: GREEN record without pristine checkout")
	}
	if !errors.Is(err, worktree.ErrNonPristineGate) {
		t.Fatalf("expected ErrNonPristineGate, got %v", err)
	}
}

func TestRequireLandingGateRefusesMarcelocantosReplaceInGoMod(t *testing.T) {
	base := sharedClone(t)
	wt, err := worktree.Ensure(base, "jv-replace")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "go.mod"), "module example.com/app\n\ngo 1.26\n\nreplace github.com/marcelocantos/claudia => ../claudia\n")
	git(t, wt, "add", "go.mod")
	git(t, wt, "commit", "-q", "-m", "jv-replace adds sibling replace")
	sha := git(t, wt, "rev-parse", "HEAD")

	gateDir := t.TempDir()
	saveGateRecord(t, gateDir, "g-ok", sha, gate.VerdictGreen, time.Now())

	_, err = worktree.RequireLandingGate(base, "jv-replace", gateDir, "")
	if err == nil {
		t.Fatal("expected refusal for committed marcelocantos replace")
	}
	if !errors.Is(err, worktree.ErrMarcelocantosReplace) {
		t.Fatalf("expected ErrMarcelocantosReplace, got %v", err)
	}
}

func TestRequireLandingGateLandsWithPristineGreenAndNoReplace(t *testing.T) {
	base := sharedClone(t)
	wt, err := worktree.Ensure(base, "jv-clean")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	writeFile(t, filepath.Join(wt, "go.mod"), "module example.com/app\n\ngo 1.26\n")
	git(t, wt, "add", "go.mod")
	git(t, wt, "commit", "-q", "-m", "jv-clean go.mod only")
	sha := git(t, wt, "rev-parse", "HEAD")

	gateDir := t.TempDir()
	saveGateRecord(t, gateDir, "g-clean", sha, gate.VerdictGreen, time.Now())

	rec, err := worktree.RequireLandingGate(base, "jv-clean", gateDir, "")
	if err != nil {
		t.Fatalf("RequireLandingGate: %v", err)
	}
	if rec == nil || rec.ID != "g-clean" {
		t.Fatalf("expected pristine GREEN record, got %+v", rec)
	}
	if _, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-clean"}); err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	mustReach(t, base, sha)
}

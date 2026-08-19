// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoalTargetIDs(t *testing.T) {
	t.Parallel()
	got := GoalTargetIDs("SPAWN T512 + 🎯T520 and T527; skip noise")
	want := []string{"T512", "T520", "T527"}
	if len(got) != len(want) {
		t.Fatalf("ids=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids[%d]=%q want %q", i, got[i], want[i])
		}
	}
	if GoalTargetIDs("no targets here") != nil {
		t.Fatal("empty target set should be nil")
	}
}

func TestGoalMissionEvidencedCompleteAllAchieved(t *testing.T) {
	t.Parallel()
	goal := "SPAWN workers for 🎯T512, T520, and T527 until achieved"
	status := map[string]string{
		"T512": "achieved",
		"T520": "achieved",
		"T527": "achieved",
	}
	if !GoalMissionEvidencedComplete(goal, status) {
		t.Fatal("all three achieved must evidence complete")
	}
	if SessionGoalContinueAllowed(goal, status) {
		t.Fatal("Continue must not be allowed when mission complete")
	}
}

func TestGoalMissionEvidencedCompleteOneStillIdentified(t *testing.T) {
	t.Parallel()
	goal := "SPAWN T512 + T520 + T527"
	status := map[string]string{
		"T512": "achieved",
		"T520": "achieved",
		"T527": "identified",
	}
	if GoalMissionEvidencedComplete(goal, status) {
		t.Fatal("one identified target must keep mission open")
	}
	if !SessionGoalContinueAllowed(goal, status) {
		t.Fatal("Continue must stay allowed while a named target is identified")
	}
}

func TestGoalMissionEvidencedCompleteMissingStatusKeepsOpen(t *testing.T) {
	t.Parallel()
	goal := "Achieve 🎯T512 and T520"
	status := map[string]string{"T512": "achieved"}
	if GoalMissionEvidencedComplete(goal, status) {
		t.Fatal("missing T520 status must not close")
	}
}

func TestGoalMissionEvidencedCompleteNoNamedTargets(t *testing.T) {
	t.Parallel()
	goal := "Continue the assigned work until it is finished."
	status := map[string]string{"T512": "achieved"}
	if GoalMissionEvidencedComplete(goal, status) {
		t.Fatal("Goal with no TargetIDs cannot close from ledger alone")
	}
	if !SessionGoalContinueAllowed(goal, status) {
		t.Fatal("Continue stays allowed when Goal names no targets")
	}
}

func TestLoadGoalTargetStatuses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := []byte(`targets:
  T512:
    name: a
    status: achieved
  T520:
    name: b
    status: identified
  T527:
    name: c
    status: achieved
`)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadGoalTargetStatuses(dir, "SPAWN T512 T520 T527")
	if got["T512"] != "achieved" || got["T520"] != "identified" || got["T527"] != "achieved" {
		t.Fatalf("statuses=%v", got)
	}
	if GoalMissionEvidencedComplete("SPAWN T512 T520 T527", got) {
		t.Fatal("T520 identified must keep open")
	}
	got["T520"] = "achieved"
	if !GoalMissionEvidencedComplete("SPAWN T512 T520 T527", got) {
		t.Fatal("all achieved must close")
	}
}

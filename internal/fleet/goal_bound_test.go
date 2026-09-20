// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"testing"
)

// houseStyleGoal is the shape that made 🎯T738 the common case rather than
// an edge case: a spawn brief for one bound target whose house-rules
// paragraph cites unrelated prior art. Every brief in this repo looks like
// this, so under the old all-ids scan no brief could ever close.
const houseStyleGoal = `You are jv-t717-replay-digest on 🎯T717 in the jevons repo.
House rules: commit only your own paths (🎯T377); shared hot files are
compare-and-swap (🎯T376); a [killed] is the harness tool timeout (🎯T712).
Delivery to the parent is the daemon path (🎯T690). See also 🎯T710.`

// TestT738BoundTargetClosesDespiteIncidentalIDs is the row's named hermetic:
// T717 achieved, T710 still open in a house-rule sentence, mission closes.
func TestT738BoundTargetClosesDespiteIncidentalIDs(t *testing.T) {
	t.Parallel()
	status := map[string]string{
		"T717": "achieved",
		"T710": "identified",
		"T377": "achieved",
		"T376": "achieved",
		"T712": "identified",
		"T690": "achieved",
	}
	if !GoalMissionComplete("T717", houseStyleGoal, status) {
		t.Fatal("bound T717 achieved must close the mission; incidental ids are prior art")
	}
	if GoalMissionContinueAllowed("T717", houseStyleGoal, status) {
		t.Fatal("Continue must not be injected once the bound target is achieved")
	}
	// The old contract, kept here as the control: scanning every id in the
	// same Goal still refuses to close, which is precisely the defect.
	if GoalMissionEvidencedComplete(houseStyleGoal, status) {
		t.Fatal("control: the all-ids scan is expected to refuse this Goal")
	}
}

// TestT738BoundTargetStillOpenKeepsMission guards the other direction: the
// fix must not close a seat whose own target is unfinished, however many
// incidental ids around it are achieved.
func TestT738BoundTargetStillOpenKeepsMission(t *testing.T) {
	t.Parallel()
	status := map[string]string{
		"T717": "converging",
		"T710": "achieved",
		"T377": "achieved",
		"T376": "achieved",
		"T712": "achieved",
		"T690": "achieved",
	}
	if GoalMissionComplete("T717", houseStyleGoal, status) {
		t.Fatal("bound target converging must keep the mission open")
	}
	if !GoalMissionContinueAllowed("T717", houseStyleGoal, status) {
		t.Fatal("Continue stays allowed while the bound target is open")
	}
}

// TestT738BoundTargetUnknownKeepsMission: a bound id with no ledger row is
// not evidence of completion. Omission keeps Continue allowed, matching the
// scan branch's own treatment of a missing status.
func TestT738BoundTargetUnknownKeepsMission(t *testing.T) {
	t.Parallel()
	if GoalMissionComplete("T717", houseStyleGoal, map[string]string{"T710": "achieved"}) {
		t.Fatal("unknown bound status must not close")
	}
	if GoalMissionComplete("T717", houseStyleGoal, nil) {
		t.Fatal("nil status map must not close")
	}
}

// TestT738BoundTargetSetAsideCloses: set_aside is a closed status, so a seat
// bound to a target the owner shelved stops rather than burning on.
func TestT738BoundTargetSetAsideCloses(t *testing.T) {
	t.Parallel()
	if !GoalMissionComplete("🎯t717", houseStyleGoal, map[string]string{"T717": "set_aside"}) {
		t.Fatal("set_aside bound target must close, and the id must normalise")
	}
}

// TestT738NoBoundTargetFallsBackToScan pins the branch the old contract got
// right. With no TargetID the Goal scan is the only signal there is, so its
// semantics — every named id must be closed, and a Goal naming none cannot
// close from the ledger alone — are preserved exactly.
func TestT738NoBoundTargetFallsBackToScan(t *testing.T) {
	t.Parallel()
	open := map[string]string{"T512": "achieved", "T520": "identified"}
	closed := map[string]string{"T512": "achieved", "T520": "achieved"}
	goal := "SPAWN workers for 🎯T512 and T520"

	if GoalMissionComplete("", goal, open) {
		t.Fatal("unbound seat: one open named id must keep the mission open")
	}
	if !GoalMissionComplete("", goal, closed) {
		t.Fatal("unbound seat: all named ids closed must close the mission")
	}
	if GoalMissionComplete("  ", "Continue the assigned work until it is finished.", closed) {
		t.Fatal("unbound seat: a Goal naming no ids cannot close from the ledger alone")
	}
}

// TestT738LoadGoalMissionStatusesLoadsBoundID: a brief addressed to a seat
// need not name that seat's target. LoadGoalTargetStatuses alone would then
// return a map with no entry for the one id that decides the mission.
func TestT738LoadGoalMissionStatusesLoadsBoundID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := []byte(`targets:
  T717:
    name: bound
    status: achieved
  T710:
    name: incidental
    status: identified
`)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	goal := "Fix the thing. House rule: see 🎯T710."

	scanOnly := LoadGoalTargetStatuses(dir, goal)
	if _, ok := scanOnly["T717"]; ok {
		t.Fatal("control: the scan cannot see a bound id the Goal never names")
	}

	got := LoadGoalMissionStatuses(dir, "T717", goal)
	if got["T717"] != "achieved" || got["T710"] != "identified" {
		t.Fatalf("statuses=%v", got)
	}
	if !GoalMissionComplete("T717", goal, got) {
		t.Fatal("bound achieved must close even though T710 is still identified")
	}
}

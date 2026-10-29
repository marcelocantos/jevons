// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package poidlenudge

import (
	"testing"
	"time"
)

// TestT1266OpenChildAndStoppedWorkerNudgeThenFault is the acceptance
// hermetic: a PO fixture with an open child target and a stopped worker
// with a 1h queued message gets exactly one nudge, then one owner fault.
func TestT1266OpenChildAndStoppedWorkerNudgeThenFault(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	obs := POObs{
		Name:  "pigeon-po",
		Alive: true,
		Idle:  true,
		Targets: []TargetObs{
			{ID: "T66.2"},
		},
		StoppedWorkers: []StoppedWorkerObs{
			{Agent: "jv-t66.2-worker", Target: "T66.2", QueuedAge: time.Hour},
		},
	}

	res, st := Classify(obs, State{}, now, DefaultQueueThreshold, DefaultFaultGrace)
	if res.Verdict != VerdictNudge {
		t.Fatalf("first cycle verdict = %s, want nudge", res.Verdict)
	}
	if len(res.StalledTargets) != 1 || res.StalledTargets[0] != "T66.2" {
		t.Fatalf("stalled targets = %v, want [T66.2]", res.StalledTargets)
	}
	if len(res.StoppedAgents) != 1 || res.StoppedAgents[0] != "jv-t66.2-worker" {
		t.Fatalf("stopped agents = %v, want [jv-t66.2-worker]", res.StoppedAgents)
	}

	// Same cycle immediately after: nudged, not yet past fault grace.
	res2, st2 := Classify(obs, st, now.Add(time.Minute), DefaultQueueThreshold, DefaultFaultGrace)
	if res2.Verdict != VerdictOK {
		t.Fatalf("second cycle (still within grace) verdict = %s, want ok", res2.Verdict)
	}

	// Nothing changed; well past the fault grace since the nudge.
	res3, _ := Classify(obs, st2, now.Add(2*time.Hour), DefaultQueueThreshold, DefaultFaultGrace)
	if res3.Verdict != VerdictFault {
		t.Fatalf("third cycle (past fault grace) verdict = %s, want fault", res3.Verdict)
	}
}

// TestT1266OwnerQuestionGetsNeither: a child waiting on an owner question
// must never nudge or fault, however long it sits.
func TestT1266OwnerQuestionGetsNeither(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	obs := POObs{
		Name:  "claudia-po",
		Alive: true,
		Idle:  true,
		Targets: []TargetObs{
			{ID: "T234", OwnerQuestion: true},
		},
	}

	st := State{}
	for i := 0; i < 3; i++ {
		var res Result
		res, st = Classify(obs, st, now.Add(time.Duration(i)*2*time.Hour), DefaultQueueThreshold, DefaultFaultGrace)
		if res.Verdict != VerdictOK {
			t.Fatalf("cycle %d verdict = %s, want ok (owner question pending)", i, res.Verdict)
		}
	}
}

// TestT1266ParkedDependencyGetsNeither mirrors the owner-question case for
// a parked/design-gated target.
func TestT1266ParkedDependencyGetsNeither(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	obs := POObs{
		Name:    "jevons-po",
		Alive:   true,
		Idle:    true,
		Targets: []TargetObs{{ID: "T9", Parked: true}},
	}
	res, _ := Classify(obs, State{}, now, DefaultQueueThreshold, DefaultFaultGrace)
	if res.Verdict != VerdictOK {
		t.Fatalf("verdict = %s, want ok (parked dependency)", res.Verdict)
	}
}

// TestT1266BlockedDependencyGetsNeither: an open depends_on must not count
// as unblocked ready work.
func TestT1266BlockedDependencyGetsNeither(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	obs := POObs{
		Name:    "jevons-po",
		Alive:   true,
		Idle:    true,
		Targets: []TargetObs{{ID: "T9", Blocked: true}},
	}
	res, _ := Classify(obs, State{}, now, DefaultQueueThreshold, DefaultFaultGrace)
	if res.Verdict != VerdictOK {
		t.Fatalf("verdict = %s, want ok (blocked dependency)", res.Verdict)
	}
}

// TestT1266EngagedTargetGetsNeither: a live worker bound to the target
// means it is not stalled, even though the PO is idle.
func TestT1266EngagedTargetGetsNeither(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	obs := POObs{
		Name:    "jevons-po",
		Alive:   true,
		Idle:    true,
		Targets: []TargetObs{{ID: "T9", Engaged: true}},
	}
	res, _ := Classify(obs, State{}, now, DefaultQueueThreshold, DefaultFaultGrace)
	if res.Verdict != VerdictOK {
		t.Fatalf("verdict = %s, want ok (engaged target)", res.Verdict)
	}
}

// TestT1266QueuedBelowThresholdGetsNeither: a stopped worker's message must
// clear the age threshold before it counts.
func TestT1266QueuedBelowThresholdGetsNeither(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	obs := POObs{
		Name:  "jevons-po",
		Alive: true,
		Idle:  true,
		StoppedWorkers: []StoppedWorkerObs{
			{Agent: "jv-x", Target: "T9", QueuedAge: 5 * time.Minute},
		},
	}
	res, _ := Classify(obs, State{}, now, DefaultQueueThreshold, DefaultFaultGrace)
	if res.Verdict != VerdictOK {
		t.Fatalf("verdict = %s, want ok (queued age below threshold)", res.Verdict)
	}
}

// TestT1266BusyPODefersJudgment: a PO mid-turn is not judged this cycle.
func TestT1266BusyPODefersJudgment(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	obs := POObs{
		Name:    "jevons-po",
		Alive:   true,
		Idle:    false,
		Targets: []TargetObs{{ID: "T9"}},
	}
	res, _ := Classify(obs, State{}, now, DefaultQueueThreshold, DefaultFaultGrace)
	if res.Verdict != VerdictOK {
		t.Fatalf("verdict = %s, want ok (PO busy)", res.Verdict)
	}
}

// TestT1266CapacityCriticalDefersNotDrops: under critical capacity pressure
// the symptom is deferred and the clock does not restart once pressure
// clears.
func TestT1266CapacityCriticalDefersNotDrops(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	obs := POObs{
		Name:             "jevons-po",
		Alive:            true,
		Idle:             true,
		Targets:          []TargetObs{{ID: "T9"}},
		CapacityCritical: true,
	}
	res, st := Classify(obs, State{}, now, DefaultQueueThreshold, DefaultFaultGrace)
	if res.Verdict != VerdictDeferred {
		t.Fatalf("verdict = %s, want deferred", res.Verdict)
	}
	if !st.NudgedAt.IsZero() {
		t.Fatalf("state should be untouched while deferred")
	}

	// Pressure clears: nudges immediately rather than restarting a wait.
	obs.CapacityCritical = false
	res2, st2 := Classify(obs, st, now.Add(time.Minute), DefaultQueueThreshold, DefaultFaultGrace)
	if res2.Verdict != VerdictNudge {
		t.Fatalf("verdict after pressure clears = %s, want nudge", res2.Verdict)
	}
	if st2.NudgedAt.IsZero() {
		t.Fatalf("expected NudgedAt to be set")
	}
}

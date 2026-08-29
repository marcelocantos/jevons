// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import "testing"

// 🎯T565: a declared wait on a tracked background gate is progress in
// flight, not an idle gap for the impatience ladder to re-pressure.
func TestT565WaitingOnGateIsSatisfied(t *testing.T) {
	o := Observation{Name: "w", Phase: "idle", ProcessRunning: true, TargetID: "T1", MissionOpen: true, WaitingOnGate: true}
	cond, kind, reason := ClassifyObservation(o)
	if cond != ConditionSatisfied || kind != "" || reason != "waiting_on_tracked_gate" {
		t.Fatalf("got %s/%s/%s", cond, kind, reason)
	}
	o.WaitingOnGate = false
	if cond, _, _ := ClassifyObservation(o); cond != ConditionGap {
		t.Fatalf("control: without the wait the idle is a gap, got %s", cond)
	}
	// A dead process is a corpse whatever its last words were.
	o.WaitingOnGate, o.ProcessRunning = true, false
	if _, kind, _ := ClassifyObservation(o); kind != GapKindDeadHandle {
		t.Fatalf("dead handle outranks a declared wait, got %s", kind)
	}
}

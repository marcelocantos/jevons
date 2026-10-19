// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/pofanout"
)

// 🎯T713 hermetic oracle: fixture registry + ledger through the HTTP-facing
// snapshot. Red against the pre-fix tree, where the verdict lived only inside
// the overseer's chat and GET /api/fleet-health did not exist.

func TestT713IdlePOOnReadyLeavesReportsFault(t *testing.T) {
	s, dir := poFanoutFixture(t, readyLedger)
	setPhase(s, "jevons-po", "idle", time.Time{})
	alive := map[string]bool{"jevons-po": true}
	t0 := time.Date(2026, 9, 21, 0, 34, 0, 0, time.UTC)

	// First sample starts grace — not yet a fault.
	snap := s.refreshFleetHealth(dir, "jevons", t0, func(n string) bool { return alive[n] })
	if snap.Stale {
		t.Fatal("fresh compute must not be stale")
	}
	po := findPOHealth(snap, "jevons-po")
	if po == nil {
		t.Fatal("snapshot must include the PO")
	}
	if po.Fault {
		t.Fatalf("within grace must not fault: %+v", po)
	}
	if po.Verdict != string(pofanout.VerdictWithinGrace) {
		t.Fatalf("verdict=%q want within_grace", po.Verdict)
	}

	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	snap = s.refreshFleetHealth(dir, "jevons", past, func(n string) bool { return alive[n] })
	po = findPOHealth(snap, "jevons-po")
	if po == nil || !po.Fault {
		t.Fatalf("idle PO on ready leaves must report fault: %+v", po)
	}
	if po.Reason != "idle_on_ready_leaves" {
		t.Fatalf("reason=%q want idle_on_ready_leaves", po.Reason)
	}
	if po.Verdict != string(pofanout.VerdictStalled) {
		t.Fatalf("verdict=%q want stalled", po.Verdict)
	}
	if po.ReadyCount < 2 {
		t.Fatalf("ready_count=%d want ≥2 stranded leaves", po.ReadyCount)
	}
	if !snap.Fault {
		t.Fatal("top-level fault must be true when any PO faults")
	}
}

func TestT713AllGatedFrontierIsNotFault(t *testing.T) {
	s, dir := poFanoutFixture(t, gatedLedger)
	setPhase(s, "jevons-po", "idle", time.Time{})
	alive := map[string]bool{"jevons-po": true}
	t0 := time.Date(2026, 9, 21, 0, 34, 0, 0, time.UTC)
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)

	snap := s.refreshFleetHealth(dir, "jevons", past, func(n string) bool { return alive[n] })
	po := findPOHealth(snap, "jevons-po")
	if po == nil {
		t.Fatal("snapshot must include the PO")
	}
	if po.Fault {
		t.Fatalf("all-gated frontier must not fault: %+v", po)
	}
	if po.Blocker != pofanout.BlockerAllGated && po.Reason != "only_gated_or_engaged" && po.Reason != "empty_frontier" {
		// sleep_ok with all_gated blocker or only_gated_or_engaged reason
		if po.Verdict != string(pofanout.VerdictSleepOK) {
			t.Fatalf("want sleep_ok with gating reason; got %+v", po)
		}
	}
	if snap.Fault {
		t.Fatal("top-level fault must be false")
	}
}

func TestT713POWithLiveChildrenHealthyOnEngagedFrontier(t *testing.T) {
	alive := map[string]bool{"jevons-po": true, "jv-t500-a": true, "jv-t501-b": true}
	s, dir := poFanoutFixture(t, readyLedger)
	for name, target := range map[string]string{"jv-t500-a": "T500", "jv-t501-b": "T501"} {
		if err := s.registry.Register(claudia.AgentDef{
			Name: name, WorkDir: dir, SessionID: name, Parent: "jevons-po",
			Purpose: claudia.PurposeWork, TargetID: target,
		}); err != nil {
			t.Fatal(err)
		}
	}
	setPhase(s, "jevons-po", "idle", time.Time{})
	t0 := time.Date(2026, 9, 21, 0, 34, 0, 0, time.UTC)
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)

	snap := s.refreshFleetHealth(dir, "jevons", past, func(n string) bool { return alive[n] })
	po := findPOHealth(snap, "jevons-po")
	if po == nil {
		t.Fatal("snapshot must include the PO")
	}
	if po.Fault {
		t.Fatalf("fully engaged frontier must not fault: %+v", po)
	}
	if po.LiveWorkChildren < 2 {
		t.Fatalf("live_work_children=%d want ≥2", po.LiveWorkChildren)
	}
	if po.Verdict != string(pofanout.VerdictSleepOK) {
		t.Fatalf("verdict=%q want sleep_ok", po.Verdict)
	}
}

func TestT713TurnNoFanoutDistinguishedFromAnswered(t *testing.T) {
	s, dir := poFanoutFixture(t, readyLedger)
	alive := map[string]bool{"jevons-po": true}
	t0 := time.Date(2026, 9, 21, 0, 34, 0, 0, time.UTC)

	// Turn in flight, then ends with zero children → turn_no_fanout.
	setPhase(s, "jevons-po", "working", t0)
	s.refreshFleetHealth(dir, "jevons", t0, func(n string) bool { return alive[n] })
	t1 := t0.Add(time.Minute)
	setPhase(s, "jevons-po", "idle", t1)
	s.refreshFleetHealth(dir, "jevons", t1, func(n string) bool { return alive[n] })
	past := t1.Add(DefaultSentinelMechanicalGrace + time.Minute)
	snap := s.refreshFleetHealth(dir, "jevons", past, func(n string) bool { return alive[n] })
	po := findPOHealth(snap, "jevons-po")
	if po == nil || !po.Fault {
		t.Fatalf("turn ended with zero children must fault: %+v", po)
	}
	if po.Verdict != string(pofanout.VerdictTurnNoFanout) {
		t.Fatalf("verdict=%q want turn_no_fanout", po.Verdict)
	}
	if po.Reason != "turn_ended_zero_new_children" {
		t.Fatalf("reason=%q", po.Reason)
	}

	// Answered arm: spawn during the turn.
	s2, dir2 := poFanoutFixture(t, readyLedger)
	alive2 := map[string]bool{"jevons-po": true}
	setPhase(s2, "jevons-po", "working", t0)
	s2.refreshFleetHealth(dir2, "jevons", t0, func(n string) bool { return alive2[n] })
	if err := s2.registry.Register(claudia.AgentDef{
		Name: "jv-t500-a", WorkDir: dir2, SessionID: "w1", Parent: "jevons-po",
		Purpose: claudia.PurposeWork, TargetID: "T500",
	}); err != nil {
		t.Fatal(err)
	}
	alive2["jv-t500-a"] = true
	setPhase(s2, "jevons-po", "idle", t1)
	s2.refreshFleetHealth(dir2, "jevons", t1, func(n string) bool { return alive2[n] })
	snap2 := s2.refreshFleetHealth(dir2, "jevons", past, func(n string) bool { return alive2[n] })
	po2 := findPOHealth(snap2, "jevons-po")
	if po2 == nil {
		t.Fatal("answered snapshot must include the PO")
	}
	if po2.Fault {
		t.Fatalf("PO that answered must not fault: %+v", po2)
	}
	// After answering, either answered (if still carrying turn evidence) or
	// sleep_ok once the remaining leaf is engaged / grace settles. The
	// load-bearing distinction is: not turn_no_fanout and not stalled.
	if po2.Verdict == string(pofanout.VerdictTurnNoFanout) || po2.Verdict == string(pofanout.VerdictStalled) {
		t.Fatalf("answered PO must not carry a fault verdict: %+v", po2)
	}
}

func TestT713StaleSnapshotNeverHealthy(t *testing.T) {
	s, dir := poFanoutFixture(t, readyLedger)
	setPhase(s, "jevons-po", "idle", time.Time{})
	alive := map[string]bool{"jevons-po": true}
	t0 := time.Date(2026, 9, 21, 0, 34, 0, 0, time.UTC)

	// Never sampled → stale empty.
	empty := s.FleetHealth(t0)
	if !empty.Stale {
		t.Fatal("never-sampled must be stale")
	}
	if empty.Fault {
		t.Fatal("never-sampled must not claim fault (or healthy) without evidence")
	}

	// Fresh compute, then read far in the future → stale, even if last verdict
	// was sleep_ok (gated) — never rewrite as healthy.
	s2, dir2 := poFanoutFixture(t, gatedLedger)
	setPhase(s2, "jevons-po", "idle", time.Time{})
	s2.refreshFleetHealth(dir2, "jevons", t0, func(n string) bool { return alive[n] })
	fresh := s2.FleetHealth(t0)
	if fresh.Stale {
		t.Fatal("just-computed must be fresh")
	}
	if fresh.Fault {
		t.Fatalf("gated frontier must not fault while fresh: %+v", fresh)
	}
	later := t0.Add(FleetHealthStaleAfter + time.Second)
	stale := s2.FleetHealth(later)
	if !stale.Stale {
		t.Fatal("snapshot past FleetHealthStaleAfter must be stale")
	}
	// Stale must still carry the last verdict, not invent healthy emptiness.
	if findPOHealth(stale, "jevons-po") == nil {
		t.Fatal("stale snapshot must retain last PO readings")
	}
	// And must not clear the fault flag of a previously-faulting snap either.
	s.refreshFleetHealth(dir, "jevons", t0, func(n string) bool { return alive[n] })
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	s.refreshFleetHealth(dir, "jevons", past, func(n string) bool { return alive[n] })
	faulty := s.FleetHealth(past)
	if !faulty.Fault {
		t.Fatal("control: ready frontier past grace must fault")
	}
	staleFault := s.FleetHealth(past.Add(FleetHealthStaleAfter + time.Second))
	if !staleFault.Stale {
		t.Fatal("aged faulting snap must be stale")
	}
	if !staleFault.Fault {
		t.Fatal("stale must not rewrite a fault as healthy")
	}
}

func findPOHealth(snap FleetHealthSnapshot, name string) *POHealth {
	for i := range snap.POs {
		if snap.POs[i].Name == name {
			return &snap.POs[i]
		}
	}
	return nil
}

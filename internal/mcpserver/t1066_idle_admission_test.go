// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T1066: idle-nudge / fleet-health must not start or rehydrate an auto
// seat while EITHER config frontier_consume.disabled OR fleet-wide
// blocked_owner intent says do not run. The 2026-10-11 escalation set only
// the config flag (an owner stabilization pause), not the intent, and
// jv-t1020-auto / jv-t1008-auto were rehydrated anyway — the config signal
// and the intent signal are independent and both must gate every revive
// path.

// --- gate 1: config frontier_consume.disabled (🎯T407) ---

// TestT1066AutoSpawnPausedBlocksAutoStartRelaunch is the direct regression
// for the dead-handle sweep named in the brief (sentinel.go /
// t662_seat_stops.go both call recoverDeadHandles/sweepDeadAgents): an
// AutoStart durable agent's dead handle is left alone — not re-Launched —
// while frontier_consume.disabled is true, even though fleet intent is
// plain Working (the incident shape: no intent was ever recorded).
func TestT1066AutoSpawnPausedBlocksAutoStartRelaunch(t *testing.T) {
	f := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "jv-t1066-durable", AutoStart: true, Purpose: claudia.PurposeWork}},
		hasProc: map[string]bool{"jv-t1066-durable": true},
		alive:   map[string]bool{"jv-t1066-durable": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, true /* autoSpawnPaused */)
	if len(reps) != 1 {
		t.Fatalf("reps=%+v want exactly one report", reps)
	}
	rep := reps[0]
	if rep.Recovered {
		t.Fatalf("rep=%+v want Recovered=false while frontier_consume is disabled", rep)
	}
	if rep.Declined != "frontier_consume_disabled" {
		t.Fatalf("rep.Declined=%q want frontier_consume_disabled", rep.Declined)
	}
	if len(f.launches) != 0 {
		t.Fatalf("launches=%v want none — AutoStart reg.Launch must not fire while paused", f.launches)
	}
	// Control: the same dead AutoStart handle recovers normally once the
	// config pause lifts (autoSpawnPaused=false), proving the gate — and
	// not some unrelated defect — is what suppressed the relaunch above.
	f2 := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "jv-t1066-durable", AutoStart: true, Purpose: claudia.PurposeWork}},
		hasProc: map[string]bool{"jv-t1066-durable": true},
		alive:   map[string]bool{"jv-t1066-durable": false},
	}
	reps2 := sweepDeadAgents(f2, "jevons", fleetintent.Snapshot{}, false)
	if len(reps2) != 1 || !reps2[0].Recovered {
		t.Fatalf("control: reps=%+v want Recovered=true once unpaused", reps2)
	}
	if len(f2.launches) != 1 || f2.launches[0] != "jv-t1066-durable" {
		t.Fatalf("control: launches=%v want [jv-t1066-durable]", f2.launches)
	}
}

// TestT1066AutoSpawnPausedLeavesNonAutoStartSeatsAlone pins the brief's
// scope: the config gate targets the AutoStart reg.Launch branch. A dead
// non-AutoStart work seat still takes its ordinary 🎯T544 remove path —
// config-disabled is not stretched into suppressing removal cleanup.
func TestT1066AutoSpawnPausedLeavesNonAutoStartSeatsAlone(t *testing.T) {
	f := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "jv-t1066-worker", Purpose: claudia.PurposeWork}},
		hasProc: map[string]bool{"jv-t1066-worker": true},
		alive:   map[string]bool{"jv-t1066-worker": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, true)
	if len(reps) != 1 || !reps[0].Removed || reps[0].Declined != "" {
		t.Fatalf("reps=%+v want removed, not declined", reps)
	}
}

// TestT1066AutoSpawnPausedBlocksSendRehydrate is the second named dispatch
// seam: a message to a not-running agent (idle-nudge / repressure both
// funnel through ensureAgentProcess via sendToAgent → deliverByName) must
// not start/rehydrate the process while frontier_consume.disabled is true,
// independent of — and in addition to — the existing fleet-intent gate
// (🎯T408).
func TestT1066AutoSpawnPausedBlocksSendRehydrate(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t1066-auto", WorkDir: dir, SessionID: "s-t1066",
		Purpose: claudia.PurposeWork, TargetID: "T1066",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)
	s.SetAutoSpawnPaused(true)

	_, _, err = s.ensureAgentProcess("jv-t1066-auto")
	if err == nil {
		t.Fatal("send to a dead seat rehydrated it while frontier_consume.disabled")
	}
	if !strings.Contains(err.Error(), "frontier_consume.disabled") {
		t.Fatalf("error %q does not name the config pause", err)
	}
	if proc := reg.Get("jv-t1066-auto"); proc != nil && proc.Alive() {
		t.Fatal("seat is alive — silent rehydrate under config pause")
	}

	// Lifting the pause restores the ordinary 🎯T408 gate (which this test
	// does not drive to a real Launch — that starts a live provider pane;
	// T414/T408 cover that path). The point is the decline above came from
	// the new config check, not a pre-existing unconditional refusal.
	s.SetAutoSpawnPaused(false)
	dec := s.AllowFleetControl("jv-t1066-auto", fleetintent.ControlDeliverStart)
	if !dec.Allow {
		t.Fatalf("unpaused working intent still declined deliver-start: %+v", dec)
	}
}

// --- gate 2: fleet-wide blocked_owner intent (pre-existing, pinned here so
// a 🎯T1066 regression that weakens it fails loudly next to the new gate) ---

// TestT1066BlockedOwnerStillBlocksAutoStartRelaunch is the control for gate
// 1: fleet-wide blocked_owner, recorded with no config flag at all, must
// independently suppress the same AutoStart reg.Launch path.
func TestT1066BlockedOwnerStillBlocksAutoStartRelaunch(t *testing.T) {
	f := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "jv-t1066-durable2", AutoStart: true, Purpose: claudia.PurposeWork}},
		hasProc: map[string]bool{"jv-t1066-durable2": true},
		alive:   map[string]bool{"jv-t1066-durable2": false},
	}
	snap := fleetintent.Snapshot{Fleet: fleetintent.Record{State: fleetintent.BlockedOwner, By: "owner", Reason: "stabilization pause"}}
	reps := sweepDeadAgents(f, "jevons", snap, false /* config not disabled */)
	if len(reps) != 1 || reps[0].Recovered {
		t.Fatalf("reps=%+v want not recovered under fleet-wide blocked_owner", reps)
	}
	if len(f.launches) != 0 {
		t.Fatalf("launches=%v want none under fleet-wide blocked_owner", f.launches)
	}
}

// TestT1066BlockedOwnerStillBlocksSendRehydrate is the matching control for
// gate 1's send-path test.
func TestT1066BlockedOwnerStillBlocksSendRehydrate(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t1066-auto2", WorkDir: dir, SessionID: "s-t1066b",
		Purpose: claudia.PurposeWork, TargetID: "T1066",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	if err := store.SetFleet(fleetintent.BlockedOwner, "owner", "stabilization pause", now); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)
	// Config flag left false on purpose: the intent alone must be enough.

	_, _, err = s.ensureAgentProcess("jv-t1066-auto2")
	if err == nil {
		t.Fatal("send to a dead seat rehydrated it under fleet-wide blocked_owner")
	}
	if !strings.Contains(err.Error(), "intent says it should not be") {
		t.Fatalf("error %q does not name the blocked_owner intent", err)
	}
}

// --- already-queued-delivery case ---

// TestT1066QueuedDeliveryDoesNotBeginTurnAfterPause is the brief's named
// extra case: a delivery decided (classified as nudge-worthy) while the
// seat was dead and the pause was NOT yet in force must still refuse at the
// actual dispatch call once the pause activates before dispatch runs —
// classification time and dispatch time are different instants, and the
// gate must hold at dispatch. ensureAgentProcess is that one dispatch seam
// for every automatic actuator (idle-nudge push, repressure RePressure)
// that routes a not-running target through sendToAgent → deliverByName.
func TestT1066QueuedDeliveryDoesNotBeginTurnAfterPause(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t1066-queued", WorkDir: dir, SessionID: "s-t1066q",
		Purpose: claudia.PurposeWork, TargetID: "T1066",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)

	// At classification time the seat looked eligible: process dead,
	// intent working, config not yet paused — a real caller would decide
	// "nudge" here.
	dec := s.AllowFleetControl("jv-t1066-queued", fleetintent.ControlDeliverStart)
	if !dec.Allow {
		t.Fatalf("pre-pause classification unexpectedly declined: %+v", dec)
	}

	// The owner's stabilization pause lands between that decision and the
	// actuator actually dispatching the already-accepted brief.
	s.SetAutoSpawnPaused(true)

	_, minted, err := s.ensureAgentProcess("jv-t1066-queued")
	if err == nil {
		t.Fatal("dispatch after the pause began a model turn on an already-queued brief")
	}
	if minted {
		t.Fatalf("minted=%v want false — no mint/revive occurred", minted)
	}
	if !strings.Contains(err.Error(), "frontier_consume.disabled") {
		t.Fatalf("error %q does not name the config pause at dispatch time", err)
	}
	if proc := reg.Get("jv-t1066-queued"); proc != nil && proc.Alive() {
		t.Fatal("queued brief rehydrated the seat after the pause took effect")
	}
}

// --- provenance: mint vs rehydrate stay distinguishable ---

// TestT1066DeclinedAutoStartReportIsNotRecovered pins the provenance
// distinction the brief asks for at the dead-handle seam: a declined
// AutoStart relaunch is reported as Declined, never as Recovered — so a
// reader of DeadAgentReport cannot mistake "left alone" for "rehydrated".
func TestT1066DeclinedAutoStartReportIsNotRecovered(t *testing.T) {
	f := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "jv-t1066-prov", AutoStart: true, Purpose: claudia.PurposeWork}},
		hasProc: map[string]bool{"jv-t1066-prov": true},
		alive:   map[string]bool{"jv-t1066-prov": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, true)
	if len(reps) != 1 {
		t.Fatalf("reps=%+v", reps)
	}
	rep := reps[0]
	if rep.Recovered || rep.Removed {
		t.Fatalf("rep=%+v want neither Recovered nor Removed", rep)
	}
	if rep.Declined == "" {
		t.Fatal("rep.Declined empty — no provenance for why the handle was left alone")
	}
}

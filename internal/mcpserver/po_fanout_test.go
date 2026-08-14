// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/staffops"
)

// 🎯T380 harness oracle: fixture ledger + registry states through the real
// sentinel sample. Red against the pre-fix tree, where an idle PO on a frontier
// full of ready leaves produced no signal at all and read as ordinary idle.

// readyLedger is a frontier with two ordinary Build leaves and nothing gating
// them — exactly the state the jevons repo stood in when this was observed.
const readyLedger = `
schema_version: 1
project: test
targets:
  T500:
    name: ordinary ready Build leaf
    status: identified
    acceptance: ["ship hermetic fix"]
  T501:
    name: second ready Build leaf
    status: identified
    acceptance: ["ship other hermetic fix"]
`

// gatedLedger holds only leaves 🎯T325.1 blesses sleeping on.
const gatedLedger = `
schema_version: 1
project: test
targets:
  T112:
    name: design-gated hub
    status: identified
    tags: [design-gated]
    acceptance: ["x"]
  T254:
    name: parked factory
    status: identified
    tags: [parked-for-design]
    acceptance: ["x"]
  T262.4:
    name: second user needs owner
    status: identified
    tags: [needs-owner]
    acceptance: ["x"]
`

// poFanoutFixture builds a workdir holding ledger, a registry holding a PO
// (plus any extra defs), and a server wired to both. Liveness is supplied per
// sample by sampleAt, since a fixture registry has no OS processes behind it.
func poFanoutFixture(t *testing.T, ledger string, extra ...claudia.AgentDef) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	defs := append([]claudia.AgentDef{{
		Name: "jevons-po", WorkDir: dir, SessionID: "po-1", Parent: "jevons",
	}}, extra...)
	for _, d := range defs {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.idleActivity = NewIdleActivityTracker()
	return s, dir
}

// sampleAt runs one sentinel sample with liveness forced for the named agents.
func sampleAt(s *Server, dir string, alive map[string]bool, now time.Time) []staffops.Signal {
	sigs, _ := s.sampleSentinel(SentinelLoopArgs{
		Server: s, Workdir: dir,
		ProcessRunning: func(name string) bool { return alive[name] },
	}, now)
	return sigs
}

// findPOFanout returns the fan-out signal for name, or nil.
func findPOFanout(sigs []staffops.Signal, name string) *staffops.Signal {
	for i := range sigs {
		if sigs[i].Kind == "po_fanout_stall" && strings.Contains(sigs[i].Symptom, name) {
			return &sigs[i]
		}
	}
	return nil
}

// setPhase drives the activity tracker the way the ACP event stream does.
func setPhase(s *Server, name, phase string, at time.Time) {
	s.idleActivity.mu.Lock()
	defer s.idleActivity.mu.Unlock()
	s.idleActivity.by[name] = IdleActivity{Phase: phase, Updated: at}
}

// 🎯T380 acceptance (1)+(4): idle PO, ready unengaged leaves → a fault the
// overseer sees, not ordinary idle.
func TestSentinelPOIdleOnReadyFrontierIsFault(t *testing.T) {
	alive := map[string]bool{"jevons-po": true}
	s, dir := poFanoutFixture(t, readyLedger)
	setPhase(s, "jevons-po", "idle", time.Time{})

	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	if sig := findPOFanout(sampleAt(s, dir, alive, t0), "jevons-po"); sig != nil {
		t.Fatalf("first sample must start the grace clock, not fire: %+v", sig)
	}

	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	sig := findPOFanout(sampleAt(s, dir, alive, past), "jevons-po")
	if sig == nil {
		t.Fatal("idle PO on a ready frontier produced no fan-out fault")
	}
	if sig.Mechanical {
		t.Fatal("fan-out silence is not a mechanical class the harness owns")
	}
	if !strings.Contains(sig.Detail, "T500") || !strings.Contains(sig.Detail, "T501") {
		t.Fatalf("fault must cite the stranded leaves: %q", sig.Detail)
	}

	// The whole point: it reaches the overseer as a file+PO decision.
	res, _ := s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: true,
		ProcessRunning: func(name string) bool { return alive[name] },
		Now:            func() time.Time { return past.Add(time.Minute) },
	})
	found := false
	for _, d := range res.Decisions {
		if d.Signal.Kind == "po_fanout_stall" {
			found = d.Action == staffops.ActionFilePO
		}
	}
	if !found {
		t.Fatalf("fan-out fault must classify file+PO; wire=\n%s", res.WireText)
	}
	if !strings.Contains(res.WireText, "po_stall:jevons-po") {
		t.Fatalf("wire report must name the PO: \n%s", res.WireText)
	}
}

// 🎯T380 acceptance (2): an all-gated frontier is legitimate sleep.
func TestSentinelPOIdleOnGatedFrontierIsNotFault(t *testing.T) {
	alive := map[string]bool{"jevons-po": true}
	s, dir := poFanoutFixture(t, gatedLedger)
	setPhase(s, "jevons-po", "idle", time.Time{})

	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sampleAt(s, dir, alive, t0)
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	if sig := findPOFanout(sampleAt(s, dir, alive, past), "jevons-po"); sig != nil {
		t.Fatalf("gated-only frontier must read as 🎯T325.1 sleep: %+v", sig)
	}
}

// 🎯T380 acceptance (2): every ready leaf already has an implementer, so there
// is nothing for the PO to spawn. Engagement comes from the registry, through
// the same ledger scoping the frontier block uses.
func TestSentinelPOIdleWithEveryLeafEngagedIsNotFault(t *testing.T) {
	alive := map[string]bool{"jevons-po": true, "jv-t500-a": true, "jv-t501-b": true}
	s, dir := poFanoutFixture(t, readyLedger)
	// Register the implementers against the fixture workdir.
	for name, target := range map[string]string{"jv-t500-a": "T500", "jv-t501-b": "T501"} {
		if err := s.registry.Register(claudia.AgentDef{
			Name: name, WorkDir: dir, SessionID: name, Parent: "jevons-po",
			Purpose: claudia.PurposeWork, TargetID: target,
		}); err != nil {
			t.Fatal(err)
		}
	}
	setPhase(s, "jevons-po", "idle", time.Time{})

	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sampleAt(s, dir, alive, t0)
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	if sig := findPOFanout(sampleAt(s, dir, alive, past), "jevons-po"); sig != nil {
		t.Fatalf("fully engaged frontier must not fault the PO: %+v", sig)
	}
}

// 🎯T380 acceptance (3): a PO that ran a turn and ended it idle with zero new
// children is distinguishable from one that answered — the case where an
// explicit fan-out order evaporated. Escalated above a plain stall because the
// PO was demonstrably awake.
func TestSentinelPOTurnEndedWithoutChildrenEscalates(t *testing.T) {
	alive := map[string]bool{"jevons-po": true}
	s, dir := poFanoutFixture(t, readyLedger)

	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	setPhase(s, "jevons-po", "working", t0)
	sampleAt(s, dir, alive, t0)

	// Turn ends, nothing spawned.
	t1 := t0.Add(time.Minute)
	setPhase(s, "jevons-po", "idle", t1)
	sampleAt(s, dir, alive, t1)

	past := t1.Add(DefaultSentinelMechanicalGrace + time.Minute)
	sig := findPOFanout(sampleAt(s, dir, alive, past), "jevons-po")
	if sig == nil {
		t.Fatal("a turn that ended with zero new children on ready leaves is a fault")
	}
	if sig.Severity != "critical" {
		t.Fatalf("severity=%q want critical (the PO was awake and spawned nothing)", sig.Severity)
	}
	if !strings.Contains(sig.Detail, "turn_ended=true") || !strings.Contains(sig.Detail, "new_children=0") {
		t.Fatalf("detail must carry the turn evidence: %q", sig.Detail)
	}
}

// 🎯T380 acceptance (3), the other arm: the PO that answered the order.
func TestSentinelPOTurnThatSpawnedIsNotFault(t *testing.T) {
	alive := map[string]bool{"jevons-po": true}
	s, dir := poFanoutFixture(t, readyLedger)

	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	setPhase(s, "jevons-po", "working", t0)
	sampleAt(s, dir, alive, t0)

	// The turn spawns a worker, then ends.
	if err := s.registry.Register(claudia.AgentDef{
		Name: "jv-t500-a", WorkDir: dir, SessionID: "w1", Parent: "jevons-po",
		Purpose: claudia.PurposeWork, TargetID: "T500",
	}); err != nil {
		t.Fatal(err)
	}
	alive["jv-t500-a"] = true
	t1 := t0.Add(time.Minute)
	setPhase(s, "jevons-po", "idle", t1)
	sampleAt(s, dir, alive, t1)

	past := t1.Add(DefaultSentinelMechanicalGrace + time.Minute)
	if sig := findPOFanout(sampleAt(s, dir, alive, past), "jevons-po"); sig != nil {
		t.Fatalf("a PO that answered the order must not be faulted: %+v", sig)
	}
}

// A PO mid-turn is not yet decidable — no fault while a turn is in flight.
func TestSentinelPOMidTurnIsNotFault(t *testing.T) {
	alive := map[string]bool{"jevons-po": true}
	s, dir := poFanoutFixture(t, readyLedger)
	setPhase(s, "jevons-po", "working", time.Time{})

	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sampleAt(s, dir, alive, t0)
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	if sig := findPOFanout(sampleAt(s, dir, alive, past), "jevons-po"); sig != nil {
		t.Fatalf("mid-turn PO must not fault: %+v", sig)
	}
}

// A PO on another repo's ledger is not answerable for this frontier (🎯T200
// puts several POs in one registry).
func TestSentinelPOOnForeignLedgerIsNotFaulted(t *testing.T) {
	foreign := t.TempDir()
	alive := map[string]bool{"squz-po": true}
	s, dir := poFanoutFixture(t, readyLedger, claudia.AgentDef{
		Name: "squz-po", WorkDir: foreign, SessionID: "squz-1", Parent: "jevons",
	})
	setPhase(s, "squz-po", "idle", time.Time{})

	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sampleAt(s, dir, alive, t0)
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	if sig := findPOFanout(sampleAt(s, dir, alive, past), "squz-po"); sig != nil {
		t.Fatalf("foreign-ledger PO must not be faulted on this frontier: %+v", sig)
	}
}

// A dead PO is the dead_agent class (🎯T85 / fleet health), not a fan-out
// fault, and rehydration must not inherit the stale turn bookkeeping.
func TestSentinelDeadPOIsNotFanoutFault(t *testing.T) {
	alive := map[string]bool{"jevons-po": false}
	s, dir := poFanoutFixture(t, readyLedger)
	setPhase(s, "jevons-po", "idle", time.Time{})

	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sampleAt(s, dir, alive, t0)
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	if sig := findPOFanout(sampleAt(s, dir, alive, past), "jevons-po"); sig != nil {
		t.Fatalf("dead PO is the dead_agent class, not a fan-out fault: %+v", sig)
	}
}

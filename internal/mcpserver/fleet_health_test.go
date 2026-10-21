// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// fakeSweepReg drives hasProc&&!Alive without a real OS process (🎯T85).
type fakeSweepReg struct {
	defs      []claudia.AgentDef
	hasProc   map[string]bool
	alive     map[string]bool
	launches  []string
	stops     []string
	removes   []string
	launchErr error
	removeErr error
	// causes is each dead handle's exit cause (🎯T925).
	causes map[string]string
}

func (f *fakeSweepReg) List() []claudia.AgentDef { return f.defs }

func (f *fakeSweepReg) ExitCause(name string) string { return f.causes[name] }

func (f *fakeSweepReg) ProcState(name string) (bool, bool) {
	return f.hasProc[name], f.alive[name]
}

func (f *fakeSweepReg) Launch(name string) error {
	f.launches = append(f.launches, name)
	if f.launchErr != nil {
		return f.launchErr
	}
	// Successful rehydrate: handle stays, now alive.
	f.hasProc[name] = true
	f.alive[name] = true
	return nil
}

func (f *fakeSweepReg) Stop(name string) {
	f.stops = append(f.stops, name)
	f.hasProc[name] = false
	f.alive[name] = false
}

func (f *fakeSweepReg) RemoveDeadSeat(name string) error {
	f.removes = append(f.removes, name)
	if f.removeErr != nil {
		return f.removeErr
	}
	f.hasProc[name] = false
	f.alive[name] = false
	kept := f.defs[:0]
	for _, d := range f.defs {
		if d.Name != name {
			kept = append(kept, d)
		}
	}
	f.defs = kept
	return nil
}

func TestDeadRecoveryPlan(t *testing.T) {
	d, r, c, rm := deadRecoveryPlan(false, false, true, true, false, claudia.PurposeWork)
	if d || r || c || rm {
		t.Fatalf("nil proc: detect=%v recover=%v clear=%v remove=%v", d, r, c, rm)
	}
	d, r, c, rm = deadRecoveryPlan(true, true, true, true, false, claudia.PurposeWork)
	if d || r || c || rm {
		t.Fatalf("alive: detect=%v recover=%v clear=%v remove=%v", d, r, c, rm)
	}
	d, r, c, rm = deadRecoveryPlan(true, false, true, true, false, claudia.PurposeWork)
	if !d || !r || c || rm {
		t.Fatalf("autostart dead: detect=%v recover=%v clear=%v remove=%v", d, r, c, rm)
	}
	// 🎯T544: a dead work seat (purpose work or unset) is removed, not stopped.
	for _, purpose := range []string{claudia.PurposeWork, ""} {
		d, r, c, rm = deadRecoveryPlan(true, false, false, true, false, purpose)
		if !d || r || c || !rm {
			t.Fatalf("work dead purpose=%q: detect=%v recover=%v clear=%v remove=%v", purpose, d, r, c, rm)
		}
	}
	// Asides and product owners keep the clear-handle path.
	for _, purpose := range []string{claudia.PurposeAside, "product-owner"} {
		d, r, c, rm = deadRecoveryPlan(true, false, false, true, false, purpose)
		if !d || r || !c || rm {
			t.Fatalf("ephemeral dead purpose=%q: detect=%v recover=%v clear=%v remove=%v", purpose, d, r, c, rm)
		}
	}
	// Intent says stay down: neither cleared nor removed.
	d, r, c, rm = deadRecoveryPlan(true, false, false, false, false, claudia.PurposeWork)
	if !d || r || c || rm {
		t.Fatalf("intent-declined dead: detect=%v recover=%v clear=%v remove=%v", d, r, c, rm)
	}
	// 🎯T1112: intent now allows it, AutoStart, but the last stop was a
	// deliberate supervisor kill and nothing addressed this name since —
	// no relaunch, just the clear-handle path (stays stopped).
	d, r, c, rm = deadRecoveryPlan(true, false, true, true, true, claudia.PurposeWork)
	if !d || r || !c || rm {
		t.Fatalf("deliberately-stopped autostart, intent flipped: detect=%v recover=%v clear=%v remove=%v", d, r, c, rm)
	}
}

// 🎯T544: a dead non-AutoStart work seat leaves the registry — Remove is
// called, List omits it, and nothing Stops or Launches it in its place.
func TestSweepDeadAgentsWorkSeatRemoved(t *testing.T) {
	f := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "jv-t544-worker", Purpose: claudia.PurposeWork}},
		hasProc: map[string]bool{"jv-t544-worker": true},
		alive:   map[string]bool{"jv-t544-worker": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, nil, nil)
	if len(reps) != 1 || reps[0].Name != "jv-t544-worker" || !reps[0].Removed || reps[0].Recovered {
		t.Fatalf("reps=%+v want removed", reps)
	}
	if len(f.removes) != 1 || f.removes[0] != "jv-t544-worker" {
		t.Fatalf("removes=%v want [jv-t544-worker]", f.removes)
	}
	if len(f.stops) != 0 || len(f.launches) != 0 {
		t.Fatalf("stops=%v launches=%v want none", f.stops, f.launches)
	}
	for _, d := range f.List() {
		if d.Name == "jv-t544-worker" {
			t.Fatal("dead work seat still listed after sweep")
		}
	}
	if !strings.Contains(FormatDeadAgentReport(reps), "jv-t544-worker:removed") {
		t.Fatalf("report=%q", FormatDeadAgentReport(reps))
	}
}

// 🎯T544: when removal refuses, the handle is still cleared and the error
// surfaces, so a failed removal never leaves a zombie "running" hope.
func TestSweepDeadAgentsWorkSeatRemoveFailClearsHandle(t *testing.T) {
	f := &fakeSweepReg{
		defs:      []claudia.AgentDef{{Name: "worker"}},
		hasProc:   map[string]bool{"worker": true},
		alive:     map[string]bool{"worker": false},
		removeErr: errors.New("registry locked"),
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, nil, nil)
	if len(reps) != 1 || reps[0].Removed || !strings.Contains(reps[0].Error, "registry locked") {
		t.Fatalf("reps=%+v", reps)
	}
	if len(f.stops) != 1 || f.stops[0] != "worker" {
		t.Fatalf("stops=%v want [worker]", f.stops)
	}
}

// 🎯T85: Sweep with hasProc&&!Alive must call Launch (AutoStart) or Stop (ephemeral).
// Deleting the recover/clear body would fail this oracle.
func TestSweepDeadAgentsAutoStartRelaunch(t *testing.T) {
	f := &fakeSweepReg{
		defs: []claudia.AgentDef{
			{Name: "durable", AutoStart: true},
			{Name: "jevons", AutoStart: true}, // overseer skipped
		},
		hasProc: map[string]bool{"durable": true, "jevons": true},
		alive:   map[string]bool{"durable": false, "jevons": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, nil, nil)
	if len(reps) != 1 || reps[0].Name != "durable" {
		t.Fatalf("reps=%+v want only durable", reps)
	}
	if !reps[0].Recovered {
		t.Fatal("expected Recovered after Launch")
	}
	if len(f.launches) != 1 || f.launches[0] != "durable" {
		t.Fatalf("launches=%v want [durable]", f.launches)
	}
	if len(f.stops) != 0 {
		t.Fatalf("stops=%v want none on successful recover", f.stops)
	}
	// Side effect: now alive
	if hp, al := f.ProcState("durable"); !hp || !al {
		t.Fatalf("after recover hasProc=%v alive=%v", hp, al)
	}
}

func TestSweepDeadAgentsEphemeralClearsHandle(t *testing.T) {
	// Aside seats keep the clear-handle path; work seats are 🎯T544 removed.
	f := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "worker", AutoStart: false, Purpose: claudia.PurposeAside}},
		hasProc: map[string]bool{"worker": true},
		alive:   map[string]bool{"worker": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, nil, nil)
	if len(reps) != 1 || reps[0].Name != "worker" || reps[0].Recovered {
		t.Fatalf("reps=%+v", reps)
	}
	if len(f.launches) != 0 {
		t.Fatalf("ephemeral must not Launch: %v", f.launches)
	}
	if len(f.stops) != 1 || f.stops[0] != "worker" {
		t.Fatalf("stops=%v want [worker]", f.stops)
	}
	if hp, _ := f.ProcState("worker"); hp {
		t.Fatal("handle still present after Stop")
	}
}

func TestSweepDeadAgentsLaunchFailThenStop(t *testing.T) {
	f := &fakeSweepReg{
		defs:      []claudia.AgentDef{{Name: "durable", AutoStart: true}},
		hasProc:   map[string]bool{"durable": true},
		alive:     map[string]bool{"durable": false},
		launchErr: errors.New("spawn refused"),
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, nil, nil)
	if len(reps) != 1 || reps[0].Recovered || !strings.Contains(reps[0].Error, "spawn refused") {
		t.Fatalf("reps=%+v", reps)
	}
	if len(f.launches) != 1 || len(f.stops) != 1 {
		t.Fatalf("launches=%v stops=%v", f.launches, f.stops)
	}
	if hp, _ := f.ProcState("durable"); hp {
		t.Fatal("failed recover must clear handle")
	}
}

func TestSweepDeadAgentsEmptyOnHealthy(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", WorkDir: dir, SessionID: "s1", Provider: "grok", AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	// Never launched → Get nil → not a dead-handle detection.
	reps := recoverDeadHandles(reg, nil, "jevons", fleetintent.Snapshot{}, nil, nil)
	if len(reps) != 0 {
		t.Fatalf("want no dead agents, got %+v", reps)
	}
}

func TestFormatDeadAgentReport(t *testing.T) {
	s := FormatDeadAgentReport(nil)
	if s != "fleet health: no dead agents" {
		t.Fatalf("%q", s)
	}
	s = FormatDeadAgentReport([]DeadAgentReport{
		{Name: "a", Recovered: true},
		{Name: "b"},
		{Name: "c", Error: "boom"},
	})
	for _, want := range []string{"a:recovered", "b:stopped", "c:fail"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %q", want, s)
		}
	}
}

// 🎯T85: recovery must appear on tool-facing text (agent_list path), not only logs.
func TestPrependFleetHealthSurfacesOnToolResult(t *testing.T) {
	body := "worker   stopped    parent=-   /w\n"
	out := PrependFleetHealth(body, []DeadAgentReport{{Name: "worker", Recovered: false}})
	if !strings.HasPrefix(out, "fleet health: dead agents → worker:stopped\n") {
		t.Fatalf("health not prepended: %q", out)
	}
	if !strings.Contains(out, body) {
		t.Fatal("body lost")
	}
	if PrependFleetHealth(body, nil) != body {
		t.Fatal("empty reps must not change body")
	}
}

// End-to-end surface: sweep fake dead + prepend is what handleAgentList returns shape-wise.
func TestSweepThenPrependIsCallerVisible(t *testing.T) {
	f := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "worker", AutoStart: false}},
		hasProc: map[string]bool{"worker": true},
		alive:   map[string]bool{"worker": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, nil, nil)
	out := PrependFleetHealth("worker stopped\n", reps)
	// 🎯T544: a dead work seat (purpose unset reads as work) is removed,
	// not stopped, and the caller sees that outcome by name.
	if !strings.Contains(out, "worker:removed") {
		t.Fatalf("tool text missing removal: %q", out)
	}
	if len(f.defs) != 0 {
		t.Fatal("side effect Remove required: row must leave the registry")
	}
}

// 🎯T1066: reviewing jv-t1066-idle-admission/4e5b06bb for this slice — a dead
// AutoStart handle must not be relaunched while frontier_consume.disabled is
// set, but ONLY for a frontier-consume-spawned seat (name suffix "-auto").
// AutoStart is true for named workers and POs too (the real registry has no
// other distinguishing field), so gating on AutoStart alone would also hold
// hostage the crash recovery of a named worker/PO — this test drives both in
// the same sweep so that mistake fails loudly.
func TestSweepDeadAgentsHonoursAutoSpawnPausedForAutoSeatsOnly(t *testing.T) {
	f := &fakeSweepReg{
		defs: []claudia.AgentDef{
			{Name: "jv-t1020-auto", Purpose: claudia.PurposeWork, AutoStart: true},
			{Name: "jevons-po", Purpose: claudia.PurposeWork, AutoStart: true},
		},
		hasProc: map[string]bool{"jv-t1020-auto": true, "jevons-po": true},
		alive:   map[string]bool{"jv-t1020-auto": false, "jevons-po": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, func(name string) string { return AutoSeatHold(name, "", true, nil) }, nil)

	byName := map[string]DeadAgentReport{}
	for _, r := range reps {
		byName[r.Name] = r
	}
	if r := byName["jv-t1020-auto"]; r.Recovered || r.Declined != "frontier_consume_disabled" {
		t.Fatalf("auto seat report=%+v want declined=frontier_consume_disabled, not recovered", r)
	}
	if r := byName["jevons-po"]; !r.Recovered {
		t.Fatalf("named PO report=%+v want recovered — config pause must not hold a named PO's crash recovery hostage", r)
	}
	launched := map[string]bool{}
	for _, n := range f.launches {
		launched[n] = true
	}
	if launched["jv-t1020-auto"] {
		t.Fatalf("launches=%v must not include the paused auto seat", f.launches)
	}
	if !launched["jevons-po"] {
		t.Fatalf("launches=%v must still include the named PO", f.launches)
	}
}

// 🎯T1112: a row preserved by a supervisor kill (🎯T560) must not come back
// just because jevons_fleet_intent flips it to working. The ambient
// dead-handle sweep sees the same shape a silent crash leaves — a known
// process handle, no longer alive — so without the deliberate-stop signal
// it cannot tell "nobody ever stopped this on purpose" from "someone just
// killed it a minute ago and nothing has addressed it since". deliberateStop
// is that signal; the sweep must leave the handle stopped, not relaunch it.
func TestSweepDeadAgentsDeliberateStopNotRevivedByIntentFlip(t *testing.T) {
	f := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "jv-t1112-auto", AutoStart: true, Purpose: claudia.PurposeWork}},
		hasProc: map[string]bool{"jv-t1112-auto": true},
		alive:   map[string]bool{"jv-t1112-auto": false},
	}
	deliberate := func(name string) bool { return name == "jv-t1112-auto" }
	// Intent now allows it (the mass flip to "working"); nothing addressed
	// this name directly.
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, nil, deliberate)
	if len(reps) != 1 || reps[0].Name != "jv-t1112-auto" || reps[0].Recovered {
		t.Fatalf("reps=%+v want detected, not recovered", reps)
	}
	if len(f.launches) != 0 {
		t.Fatalf("launches=%v want none — a deliberate kill is not undone by an intent flip alone", f.launches)
	}
	if hp, alive := f.ProcState("jv-t1112-auto"); hp || alive {
		t.Fatalf("handle should be cleared/stopped, got hasProc=%v alive=%v", hp, alive)
	}

	// Same shape, but nothing recorded this name as deliberately stopped
	// (e.g. a genuine silent crash): the pre-existing AutoStart recovery
	// still fires.
	f2 := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "jv-t1112-crashed", AutoStart: true, Purpose: claudia.PurposeWork}},
		hasProc: map[string]bool{"jv-t1112-crashed": true},
		alive:   map[string]bool{"jv-t1112-crashed": false},
	}
	reps2 := sweepDeadAgents(f2, "jevons", fleetintent.Snapshot{}, nil, deliberate)
	if len(reps2) != 1 || !reps2[0].Recovered {
		t.Fatalf("reps2=%+v want recovered (not a deliberate stop)", reps2)
	}
	if len(f2.launches) != 1 || f2.launches[0] != "jv-t1112-crashed" {
		t.Fatalf("launches=%v want [jv-t1112-crashed]", f2.launches)
	}
}

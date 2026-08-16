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
	launchErr error
}

func (f *fakeSweepReg) List() []claudia.AgentDef { return f.defs }

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

func TestDeadRecoveryPlan(t *testing.T) {
	d, r, c := deadRecoveryPlan(false, false, true, true)
	if d || r || c {
		t.Fatalf("nil proc: detect=%v recover=%v clear=%v", d, r, c)
	}
	d, r, c = deadRecoveryPlan(true, true, true, true)
	if d || r || c {
		t.Fatalf("alive: detect=%v recover=%v clear=%v", d, r, c)
	}
	d, r, c = deadRecoveryPlan(true, false, true, true)
	if !d || !r || c {
		t.Fatalf("autostart dead: detect=%v recover=%v clear=%v", d, r, c)
	}
	d, r, c = deadRecoveryPlan(true, false, false, true)
	if !d || r || !c {
		t.Fatalf("ephemeral dead: detect=%v recover=%v clear=%v", d, r, c)
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
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{})
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
	f := &fakeSweepReg{
		defs:    []claudia.AgentDef{{Name: "worker", AutoStart: false}},
		hasProc: map[string]bool{"worker": true},
		alive:   map[string]bool{"worker": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{})
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
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{})
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
	reps := SweepDeadAgents(reg, "jevons", fleetintent.Snapshot{})
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
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{})
	out := PrependFleetHealth("worker stopped\n", reps)
	if !strings.Contains(out, "worker:stopped") {
		t.Fatalf("tool text missing recovery: %q", out)
	}
	if len(f.stops) != 1 {
		t.Fatal("side effect Stop required")
	}
}

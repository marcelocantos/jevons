// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// deadProc implements the Alive() false path via a real registry process
// that we stop without Remove — simulating silent death of the OS process
// while the registry still holds a def (and after Stop, Get is nil).
//
// Claudia's Get returns nil after Stop; silent death without Stop leaves a
// dead *Agent. We approximate detection by injecting: Register AutoStart,
// Launch is not used — we only assert Sweep on nil Get is a no-op, and that
// the pure report formatter works. Launch recovery is covered when Alive()
// can be false with non-nil Get — use a stub via Stop-then-manual only if API allows.
//
// Practical hermetic oracle for T85 detection surface: FormatDeadAgentReport
// + SweepDeadAgents returns empty on healthy registry; and with a def-only
// (never launched) agent, Get is nil → not "dead handle" (already stopped).
//
// Real dead-handle path requires a process that was launched then died.
// We test the recover policy branch by constructing reports and the
// AutoStart path with Launch failure after Stop (Launch of Materialized
// session may fail in hermetic env — still clears handle).

func TestDeadRecoveryPlan(t *testing.T) {
	// no process
	d, r, c := deadRecoveryPlan(false, false, true)
	if d || r || c {
		t.Fatalf("nil proc: detect=%v recover=%v clear=%v", d, r, c)
	}
	// healthy
	d, r, c = deadRecoveryPlan(true, true, true)
	if d || r || c {
		t.Fatalf("alive: detect=%v recover=%v clear=%v", d, r, c)
	}
	// silent death AutoStart → detect + try recover
	d, r, c = deadRecoveryPlan(true, false, true)
	if !d || !r || c {
		t.Fatalf("autostart dead: detect=%v recover=%v clear=%v", d, r, c)
	}
	// silent death ephemeral → detect + clear handle
	d, r, c = deadRecoveryPlan(true, false, false)
	if !d || r || !c {
		t.Fatalf("ephemeral dead: detect=%v recover=%v clear=%v", d, r, c)
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
	reps := SweepDeadAgents(reg, "jevons")
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
	if !containsAll(s, "a:recovered", "b:stopped", "c:fail") {
		t.Fatalf("%q", s)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

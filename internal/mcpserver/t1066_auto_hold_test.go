// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T1066: the daemon must not revive an auto-spawned seat on its own while
// the frontier is paused, or once its target is achieved. These tests drive
// the hold itself, the dead-handle relaunch sweep, and the send-triggered
// rehydrate (the road the 2026-10-11 wake wave came in by), and check that a
// held revive starts nothing and is recorded as held — never as a start.

func TestT1066AutoSeatHold(t *testing.T) {
	closed := func(tid string) bool { return tid != "T1008" } // T1008 achieved
	for _, tc := range []struct {
		name, target string
		paused       bool
		open         func(string) bool
		want         string
	}{
		{"jv-t1020-auto", "T1020", true, closed, AutoHoldPaused},
		{"jv-t1008-auto", "T1008", false, closed, AutoHoldTargetAchieved},
		{"jv-t1020-auto", "T1020", false, closed, ""},
		// No ledger reading is no reading: the seat is not held.
		{"jv-t1008-auto", "T1008", false, nil, ""},
		{"jv-t1008-auto", "", false, closed, ""},
		// A named worker or PO is never held, paused or not, achieved or not.
		{"jevons-po", "T1008", true, closed, ""},
		{"jv-t1008-review", "T1008", true, closed, ""},
	} {
		if got := AutoSeatHold(tc.name, tc.target, tc.paused, tc.open); got != tc.want {
			t.Errorf("AutoSeatHold(%s, %s, paused=%v) = %q, want %q", tc.name, tc.target, tc.paused, got, tc.want)
		}
	}
}

// A dead handle bound to an achieved target is not relaunched, outside any
// pause, and a named worker on the same target still is.
func TestT1066DeadHandleOnAnAchievedTargetIsNotRelaunched(t *testing.T) {
	f := &fakeSweepReg{
		defs: []claudia.AgentDef{
			{Name: "jv-t1008-auto", Purpose: claudia.PurposeWork, AutoStart: true, TargetID: "T1008"},
			{Name: "jevons-po", Purpose: claudia.PurposeWork, AutoStart: true, TargetID: "T1008"},
		},
		hasProc: map[string]bool{"jv-t1008-auto": true, "jevons-po": true},
		alive:   map[string]bool{"jv-t1008-auto": false, "jevons-po": false},
	}
	targets := map[string]string{"jv-t1008-auto": "T1008", "jevons-po": "T1008"}
	achieved := func(string) bool { return false }
	hold := func(name string) string { return AutoSeatHold(name, targets[name], false, achieved) }

	byName := map[string]DeadAgentReport{}
	for _, r := range sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, hold, nil) {
		byName[r.Name] = r
	}
	if r := byName["jv-t1008-auto"]; r.Recovered || r.Declined != AutoHoldTargetAchieved {
		t.Fatalf("auto seat on an achieved target: %+v, want declined %s and not recovered", r, AutoHoldTargetAchieved)
	}
	if r := byName["jevons-po"]; !r.Recovered {
		t.Fatalf("named PO: %+v, want its crash recovery to run", r)
	}
	for _, n := range f.launches {
		if n == "jv-t1008-auto" {
			t.Fatalf("launches=%v include the held auto seat", f.launches)
		}
	}
}

// t1066SendServer is a server whose registry starts seats as stubs, counting
// every launch, with a capturing event logger.
func t1066SendServer(t *testing.T, def claudia.AgentDef) (*Server, *int, *[]map[string]any) {
	t.Helper()
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	launches := 0
	var mu sync.Mutex
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Adopt: func(claudia.Config) (*claudia.Agent, error) { return nil, claudia.ErrNoSessionWindow },
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if cfg.AdoptOnly {
				return nil, claudia.ErrNoSessionWindow
			}
			mu.Lock()
			launches++
			mu.Unlock()
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	def.WorkDir, def.SessionID, def.Purpose = dir, "s-"+def.Name, claudia.PurposeWork
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)
	var events []map[string]any
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		e := map[string]any{"component": component, "decision": decision}
		for k, v := range fields {
			e[k] = v
		}
		events = append(events, e)
	})
	t.Cleanup(func() { reg.Stop(def.Name) })
	return s, &launches, &events
}

// The road the wake wave came in by: a send to a stopped auto seat. Under
// the pause, and on an achieved target, it starts nothing, so no model turn
// can begin, and the log says it was a held rehydrate on the send path.
func TestT1066SendDoesNotRehydrateAHeldAutoSeat(t *testing.T) {
	for _, tc := range []struct {
		name, hold string
		paused     bool
		achieved   bool
	}{
		{"paused", AutoHoldPaused, true, false},
		{"achieved", AutoHoldTargetAchieved, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, launches, events := t1066SendServer(t, claudia.AgentDef{Name: "jv-t1008-auto", TargetID: "T1008"})
			s.SetAutoSpawnPaused(tc.paused)
			s.SetIdlePressureHooks(IdlePressureHooks{MissionOpen: func(string) bool { return !tc.achieved }})

			_, _, err := s.ensureAgentProcess("jv-t1008-auto")
			if err == nil || !strings.Contains(err.Error(), tc.hold) {
				t.Fatalf("err = %v, want a refusal naming %s", err, tc.hold)
			}
			if *launches != 0 {
				t.Fatalf("launches = %d, want none: a held seat must not start", *launches)
			}
			if p := s.registry.Get("jv-t1008-auto"); p != nil && p.Alive() {
				t.Fatal("held seat is alive")
			}
			held := false
			for _, e := range *events {
				if e["decision"] == "revive_held" && e["path"] == "send" && e["reason"] == tc.hold && e["how"] == "rehydrate" {
					held = true
				}
				if e["decision"] == "start" || e["decision"] == "rehydrate" {
					t.Fatalf("a held revive was logged as a start: %+v", e)
				}
			}
			if !held {
				t.Fatalf("events %+v lack revive_held path=send reason=%s", *events, tc.hold)
			}
		})
	}
}

// Control: with no hold, the same send rehydrates the seat, and the log
// records it as a rehydrate on the send path, distinct from a mint.
func TestT1066SendRehydratesAnUnheldSeatAndSaysSo(t *testing.T) {
	s, launches, events := t1066SendServer(t, claudia.AgentDef{Name: "jv-t1020-auto", TargetID: "T1020"})
	s.SetIdlePressureHooks(IdlePressureHooks{MissionOpen: func(string) bool { return true }})

	if _, _, err := s.ensureAgentProcess("jv-t1020-auto"); err != nil {
		t.Fatal(err)
	}
	if *launches != 1 {
		t.Fatalf("launches = %d, want the one rehydrate", *launches)
	}
	ok := false
	for _, e := range *events {
		if e["decision"] == "rehydrate" && e["path"] == "send" && e["outcome"] == "ok" && e["how"] == "rehydrate" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("events %+v lack rehydrate path=send outcome=ok", *events)
	}
}

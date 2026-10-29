// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetpanel"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

func TestT1121APIAgentsCarriesPanelClass(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "live", WorkDir: dir, SessionID: "s-live", Purpose: claudia.PurposeWork},
		{Name: "po-dead", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, AutoStart: true},
		{Name: "reaped", WorkDir: dir, SessionID: "s-reaped", Purpose: claudia.PurposeWork},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAgent("reaped", fleetintent.Reaped, "test", "done", time.Now()); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	s.SetFleetIntentSource(store.Snapshot)
	// Dead means observed stopped. An unobserved durable seat (the minute
	// after a restart) is not yet a fault.
	seatstate.ObserveStopped(reg, "po-dead")
	rows := s.decoratePanelClass([]agentInfo{
		{Name: "live", Running: true},
		{Name: "po-dead"},
		{Name: "reaped"},
	})
	if rows[0].Class != "live" || !rows[0].Visible {
		t.Fatalf("live row = class %q visible %v", rows[0].Class, rows[0].Visible)
	}
	if rows[1].Class != "fault" || !rows[1].Visible {
		t.Fatalf("fault row = class %q visible %v", rows[1].Class, rows[1].Visible)
	}
	if rows[2].Class != "history" || rows[2].Visible {
		t.Fatalf("history row = class %q visible %v", rows[2].Class, rows[2].Visible)
	}
}

func TestT1127APIAgentsSurfacesParkedButRunningAsFault(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := reg.Register(claudia.AgentDef{
		Name: "parked-live", WorkDir: dir, SessionID: "s-parked", Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAgent("parked-live", fleetintent.Parked, "jevons-po", "park", time.Now()); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	s.SetFleetIntentSource(store.Snapshot)
	rows := s.decoratePanelClass([]agentInfo{{
		Name: "parked-live", Running: true, Status: "running", Phase: "idle",
	}})
	if rows[0].Status != fleetpanel.StatusRunningAgainstIntent {
		t.Fatalf("status = %q, want %q", rows[0].Status, fleetpanel.StatusRunningAgainstIntent)
	}
	if rows[0].Class != "fault" {
		t.Fatalf("class = %q, want fault", rows[0].Class)
	}
}

// T1263: the daemon's auto-seat hold, rather than queue age or mere presence,
// decides whether a stopped auto seat is a fault in the owner fleet tree.
func TestT1263HeldAutoSeatVersusDeliverableOwnerQueue(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"held", "deliverable"} {
		if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: t.TempDir(), SessionID: "s-" + name, Purpose: claudia.PurposeWork, AutoStart: true}); err != nil {
			t.Fatal(err)
		}
	}
	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	seatstate.ObserveStopped(reg, "held")
	seatstate.ObserveStopped(reg, "deliverable")
	s.SetAutoSeatHoldReader(func(name string) string {
		if name == "held" {
			return "frontier_consume_disabled"
		}
		return ""
	})
	rows := s.decoratePanelClass([]agentInfo{
		{Name: "held", StopReason: "stopped", StoppedAt: "2026-10-21T12:00:00Z"},
		{Name: "deliverable", StopReason: "stopped", StoppedAt: "2026-10-21T12:00:00Z"},
	})
	if rows[0].Class == "fault" || strings.Contains(rows[0].PanelLine, "should be running") {
		t.Fatalf("policy-held stale brief produced fault: %+v", rows[0])
	}
	if rows[1].Class != "fault" || !rows[1].Visible || !strings.Contains(rows[1].PanelLine, "should be running") {
		t.Fatalf("deliverable owner message lost fault: %+v", rows[1])
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatliveness"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T751 — a seat whose provider process died is visible as dead from the
// registry alone, distinguishable from a live-but-busy seat, without send.

func TestT751DeadSeatReadsStoppedNotIdleOnAgentList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t751-worker"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-dead", Provider: "claude", Materialized: true,
		Purpose: claudia.PurposeAside, AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	// Process was up and looked idle; then the provider died underneath.
	s.Seats().Observe(seatstate.Observation{
		Name: name, SessionID: "s-dead", Alive: seatstate.Yes, Phase: turnev.PhaseIdle,
		QueueDepth: seatstate.QueueUnknown, Source: "test.fixture", At: time.Now(),
	})
	s.Seats().Observe(seatstate.Observation{
		Name: name, SessionID: "s-dead", Alive: seatstate.No,
		QueueDepth: seatstate.QueueUnknown, Source: "test.process", At: time.Now(),
	})
	s.SetSeatAliveFn(func(n string) bool { return false })

	def := reg.Def(name)
	if got := observedAgentPhase(s, *def, false); got != AgentStatusStopped {
		t.Fatalf("dead seat phase=%q want %s (must not read idle while provider is gone)", got, AgentStatusStopped)
	}
	if seatliveness.Served(s.seatState(name)) != seatliveness.ServedDead {
		t.Fatalf("authority liveness=%s want dead", seatliveness.Served(s.seatState(name)))
	}
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"history": true}}}
	res, err := observedAgentList(s, context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(res)
	if !strings.Contains(text, AgentStatusStopped) {
		t.Fatalf("dead seat must not read idle/busy on agent_list:\n%s", text)
	}
	if !strings.Contains(text, "process_liveness=dead") {
		t.Fatalf("missing dead liveness marker:\n%s", text)
	}
}

func TestT751BusySeatStillLiveOnAgentList(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t751-busy"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-busy", Provider: "claude", Materialized: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.Seats().Observe(seatstate.Observation{
		Name: name, SessionID: "s-busy", Alive: seatstate.Yes, InFlight: seatstate.Yes,
		Phase: turnev.PhaseWorking, QueueDepth: seatstate.QueueUnknown, Source: "test.fixture", At: time.Now(),
	})
	s.SetSeatAliveFn(func(n string) bool { return n == name })

	res, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(res)
	if strings.Contains(text, "process_liveness=dead") {
		t.Fatalf("busy live seat must not read dead:\n%s", text)
	}
	if seatliveness.Served(s.seatState(name)) != seatliveness.ServedLive {
		t.Fatalf("authority liveness=%s want live", seatliveness.Served(s.seatState(name)))
	}
}

// A dead product owner is rehydrated by the ambient dead-handle sweep (remint).
func TestT751DeadProductOwnerIsRemintedByFleetHealth(t *testing.T) {
	f := &fakeSweepReg{
		defs: []claudia.AgentDef{
			{Name: "jevons-po", AutoStart: true, Purpose: claudia.PurposeWork},
		},
		hasProc: map[string]bool{"jevons-po": true},
		alive:   map[string]bool{"jevons-po": false},
	}
	reps := sweepDeadAgents(f, "jevons", fleetintent.Snapshot{}, nil, nil)
	if len(reps) != 1 || reps[0].Name != "jevons-po" || !reps[0].Recovered {
		t.Fatalf("dead PO must be reminted by sweep: %+v", reps)
	}
	if len(f.launches) != 1 || f.launches[0] != "jevons-po" {
		t.Fatalf("launches=%v want [jevons-po]", f.launches)
	}
}

func TestT751DeadPONoticeWhenRemintDeclined(t *testing.T) {
	reg := &fakeSweepReg{
		defs: []claudia.AgentDef{
			{Name: "jevons-po", AutoStart: true, Purpose: claudia.PurposeWork},
		},
		hasProc:   map[string]bool{"jevons-po": true},
		alive:     map[string]bool{"jevons-po": false},
		launchErr: errBrokerDown,
	}
	reps := sweepDeadAgents(reg, "jevons", fleetintent.Snapshot{}, nil, nil)
	line := FormatDeadAgentReport(reps)
	if !strings.Contains(line, "jevons-po") {
		t.Fatalf("notice must name the dead PO: %q", line)
	}
	if reps[0].Recovered {
		t.Fatal("declined remint must not read as recovered")
	}
}

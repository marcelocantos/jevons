// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T795: twelve registered claude rows with only three live must not refuse a
// claude mint at a published soft cap of 12. Stopped, reaped, born-stuck, and
// other-ledger rows do not bind the provider cap.
func TestT795SeatCountUsesLiveClaudeSeatsOnly(t *testing.T) {
	const repo = "/w/jevons"
	defs := make([]claudia.AgentDef, 0, 12)
	states := map[string]seatstate.State{}
	intents := map[string]fleetintent.State{}
	for i := 0; i < 3; i++ {
		n := fmt.Sprintf("live-%d", i)
		defs = append(defs, claudia.AgentDef{Name: n, WorkDir: repo, Provider: claudia.ProviderClaude})
		states[n] = seatstate.State{Alive: seatstate.Yes}
		intents[n] = fleetintent.Working
	}
	for i := 0; i < 4; i++ {
		n := fmt.Sprintf("stopped-%d", i)
		defs = append(defs, claudia.AgentDef{Name: n, WorkDir: repo, Provider: claudia.ProviderClaude})
		states[n] = seatstate.State{Alive: seatstate.No}
		intents[n] = fleetintent.Working
	}
	for i := 0; i < 2; i++ {
		n := fmt.Sprintf("reaped-%d", i)
		defs = append(defs, claudia.AgentDef{Name: n, WorkDir: repo, Provider: claudia.ProviderClaude})
		states[n] = seatstate.State{Alive: seatstate.Yes}
		intents[n] = fleetintent.Reaped
	}
	for i := 0; i < 2; i++ {
		n := fmt.Sprintf("born-stuck-%d", i)
		defs = append(defs, claudia.AgentDef{Name: n, WorkDir: repo, Provider: claudia.ProviderClaude})
		states[n] = seatstate.State{Alive: seatstate.Yes, BornStuck: seatstate.Yes}
		intents[n] = fleetintent.Working
	}
	defs = append(defs, claudia.AgentDef{Name: "other-repo", WorkDir: "/w/claudia", Provider: claudia.ProviderClaude})
	states["other-repo"] = seatstate.State{Alive: seatstate.Yes}
	intents["other-repo"] = fleetintent.Working

	census := activeLiveSeatCensus(defs, func(n string) seatstate.State { return states[n] },
		func(n string) fleetintent.State { return intents[n] }, repo, func(string) bool { return false })
	if census.Load["claude"] != 3 {
		t.Fatalf("live claude load = %d, want 3; names=%v", census.Load["claude"], census.Names["claude"])
	}
	wantNames := []string{"live-0", "live-1", "live-2"}
	if strings.Join(census.Names["claude"], ",") != strings.Join(wantNames, ",") {
		t.Fatalf("counted names = %v, want %v", census.Names["claude"], wantNames)
	}

	snap := CapacitySnapshot(CapacitySnapshotArgs{
		Budget: func() *cost.BudgetConfig { return &cost.BudgetConfig{MaxSessions: 20} },
		ProviderLoad: func() map[string]int { return map[string]int(census.Load) },
		ProviderLoadSeats: func() map[string][]string {
			out := map[string][]string{}
			for k, v := range census.Names {
				out[k] = append([]string(nil), v...)
			}
			return out
		},
		ProviderSoftCaps: func() map[string]int { return map[string]int{"claude": 12} },
	})
	if snap.ActiveSessions != 3 {
		t.Fatalf("session census = %d, want 3 live seats", snap.ActiveSessions)
	}
	if d := capacity.AdmitSpawnDest(capacity.SpawnWorker, "claude", snap, capacity.DefaultPolicy()); !d.Admitted() {
		t.Fatalf("3 live claude seats refused at cap 12: %+v", d)
	}

	for i := 3; i < 12; i++ {
		n := fmt.Sprintf("fill-%d", i)
		defs = append(defs, claudia.AgentDef{Name: n, WorkDir: repo, Provider: claudia.ProviderClaude})
		states[n] = seatstate.State{Alive: seatstate.Yes}
		intents[n] = fleetintent.Working
	}
	census = activeLiveSeatCensus(defs, func(n string) seatstate.State { return states[n] },
		func(n string) fleetintent.State { return intents[n] }, repo, func(string) bool { return false })
	snap = CapacitySnapshot(CapacitySnapshotArgs{
		Budget:            func() *cost.BudgetConfig { return &cost.BudgetConfig{MaxSessions: 20} },
		ProviderLoad:      func() map[string]int { return map[string]int(census.Load) },
		ProviderLoadSeats: func() map[string][]string { return census.Names },
		ProviderSoftCaps:  func() map[string]int { return map[string]int{"claude": 12} },
	})
	d := capacity.AdmitSpawnDest(capacity.SpawnWorker, "claude", snap, capacity.DefaultPolicy())
	if d.Admitted() {
		t.Fatalf("12 live claude seats should refuse: %+v", d)
	}
	if !strings.Contains(d.Detail, "counted:") || !strings.Contains(d.Detail, "live-0") {
		t.Fatalf("refusal must cite live rows by name: %q", d.Detail)
	}
}

func TestT795HarnessLoadMatchesLiveCensus(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "jevons")
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("ghost-%d", i)
		if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: repo, SessionID: "s-" + name, Provider: claudia.ProviderClaude}); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.Register(claudia.AgentDef{Name: "alive", WorkDir: repo, SessionID: "s-alive", Provider: claudia.ProviderClaude}); err != nil {
		t.Fatal(err)
	}
	auth := seatstate.New(seatstate.Args{})
	auth.BindRegistry(reg)
	auth.Observe(seatstate.Observation{Name: "alive", Alive: seatstate.Yes, Source: "test", QueueDepth: seatstate.QueueUnknown})
	s := &Server{registry: reg, workerWD: repo}
	s.SetSeats(auth)
	load := s.HarnessLoad()
	if load["claude"] != 1 {
		t.Fatalf("HarnessLoad = %v, want claude=1 with one live seat among six registered", load)
	}
}

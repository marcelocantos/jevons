// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T867: seat-count spawn refusal uses registered alive fleet processes,
// not the cost-window census of unattributed (empty-worker) owner sessions.
//
// 2026-09-25 specimen: 7 running fleet seats on /api/agents, 24 empty-worker
// cost-window rows (owner youtube grok sessions), MaxSessions 20. Spawn of
// jv-t851-global-rate was refused as "24 live seats of 20". That census was
// the cost window, not the fleet.
func TestT867EmptyWorkerCostWindowDoesNotBindSeatCount(t *testing.T) {
	// 7 registered alive fleet processes.
	defs := make([]claudia.AgentDef, 0, 7)
	for i := 0; i < 7; i++ {
		defs = append(defs, claudia.AgentDef{
			Name:     fmt.Sprintf("alive-%d", i),
			Provider: claudia.ProviderGrok,
		})
	}
	load := map[string]int(activeProcessLoad(defs, func(string) seatstate.State {
		return seatstate.State{Alive: seatstate.Yes}
	}, func(string) bool { return false }))

	// 24 empty-worker cost-window rows — the owner youtube sessions.
	costRows := make([]cost.BurnRow, 24)
	for i := range costRows {
		costRows[i] = cost.BurnRow{SessionID: fmt.Sprintf("yt-%d", i), Worker: ""}
	}

	snap := CapacitySnapshot(CapacitySnapshotArgs{
		Budget:       func() *cost.BudgetConfig { return &cost.BudgetConfig{MaxSessions: 20} },
		Cost:         func() (*cost.Snapshot, error) { return &cost.Snapshot{Sessions: costRows}, nil },
		ProviderLoad: func() map[string]int { return load },
	})
	if snap.ActiveSessions != 7 {
		t.Fatalf("ActiveSessions = %d, want 7 registered alive (not cost-window)", snap.ActiveSessions)
	}
	if snap.CostWindowSessions != 24 {
		t.Fatalf("CostWindowSessions = %d, want 24 empty-worker rows retained as diagnostic", snap.CostWindowSessions)
	}
	if snap.MaxSessions != 20 {
		t.Fatalf("MaxSessions = %d, want 20", snap.MaxSessions)
	}
	d := capacity.AdmitSpawn(capacity.SpawnWorker, snap, capacity.DefaultPolicy())
	if !d.Admitted() {
		t.Fatalf("7 alive under MaxSessions 20 refused despite 24 empty-worker cost rows: %+v", d)
	}
	if d.Reason == capacity.ReasonSeatCount {
		t.Fatalf("admitted decision still cites seat_count: %+v", d)
	}
}

// The same gate refuses when registered alive processes are at the bound,
// and the refusal cites that registered count — not the cost window.
func TestT867RegisteredAliveAtBoundRefusesCitingRegisteredCount(t *testing.T) {
	defs := make([]claudia.AgentDef, 0, 21)
	for i := 0; i < 21; i++ {
		defs = append(defs, claudia.AgentDef{
			Name:     fmt.Sprintf("alive-%d", i),
			Provider: claudia.ProviderGrok,
		})
	}
	load := map[string]int(activeProcessLoad(defs, func(string) seatstate.State {
		return seatstate.State{Alive: seatstate.Yes}
	}, func(string) bool { return false }))

	// Cost window still over the bound with empty workers — must not be
	// the refusal's cited count.
	costRows := make([]cost.BurnRow, 40)
	for i := range costRows {
		costRows[i] = cost.BurnRow{SessionID: fmt.Sprintf("yt-%d", i), Worker: ""}
	}

	snap := CapacitySnapshot(CapacitySnapshotArgs{
		Budget:       func() *cost.BudgetConfig { return &cost.BudgetConfig{MaxSessions: 20} },
		Cost:         func() (*cost.Snapshot, error) { return &cost.Snapshot{Sessions: costRows}, nil },
		ProviderLoad: func() map[string]int { return load },
	})
	if snap.ActiveSessions != 21 {
		t.Fatalf("ActiveSessions = %d, want 21 registered alive", snap.ActiveSessions)
	}
	d := capacity.AdmitSpawn(capacity.SpawnWorker, snap, capacity.DefaultPolicy())
	if d.Admitted() {
		t.Fatalf("21 registered alive at MaxSessions 20 admitted: %+v", d)
	}
	if d.Reason != capacity.ReasonSeatCount {
		t.Fatalf("reason = %q, want %s", d.Reason, capacity.ReasonSeatCount)
	}
	// Refusal must cite the registered census (21 of 20), not the 40
	// empty-worker cost-window rows.
	if !strings.Contains(d.Detail, "21 live seats of 20") {
		t.Fatalf("detail %q does not cite registered count 21 of 20", d.Detail)
	}
	if strings.Contains(d.Detail, "40 live") {
		t.Fatalf("detail %q cites cost-window count 40", d.Detail)
	}
}

// Seat-count admission is a spawn gate only: empty-worker cost rows never
// become a kill/stop/throttle target. Fleet burn is attributed workers only;
// session-count alerts notify and do not PauseWorker / KillWorker / StopFleet.
func TestT867EmptyWorkerSessionsAreNotFleetClampTargets(t *testing.T) {
	rows := make([]cost.BurnRow, 24)
	for i := range rows {
		rows[i] = cost.BurnRow{
			SessionID: fmt.Sprintf("yt-%d", i),
			Worker:    "", // unattributed owner sessions
			CostUSD:   1.0,
			Events:    1,
		}
	}
	snap := &cost.Snapshot{
		Sessions:         rows,
		GlobalUSDPerHour: 24.0, // all unattributed
		FleetUSDPerHour:  0,    // no attributed workers
		Alerts: []cost.Alert{{
			Kind:   cost.AlertSessionCount,
			Level:  cost.LevelWarn,
			Detail: "24 billable sessions in window (bound 20)",
		}},
	}
	rec := &t867Actions{}
	enf := cost.NewEnforcer(&cost.EnforcerArgs{
		Snapshot: func() (*cost.Snapshot, error) { return snap, nil },
		Config: func() *cost.BudgetConfig {
			return &cost.BudgetConfig{
				MaxSessions:      20,
				ProtectedWorkers: []string{"jevons"},
			}
		},
		Actions: rec,
		Notify:  func(cost.Level, string) {},
	})
	enf.Act(snap)
	if rec.paused != 0 || rec.killed != 0 || rec.fleetStops != 0 || rec.killSwitches != 0 {
		t.Fatalf("enforcer acted on unattributed sessions: paused=%d killed=%d fleetStops=%d killSwitches=%d",
			rec.paused, rec.killed, rec.fleetStops, rec.killSwitches)
	}
}

type t867Actions struct {
	paused, killed, fleetStops, killSwitches int
}

func (a *t867Actions) PauseWorker(string) error { a.paused++; return nil }
func (a *t867Actions) KillWorker(string) error  { a.killed++; return nil }
func (a *t867Actions) StopFleet() error         { a.fleetStops++; return nil }
func (a *t867Actions) KillSwitch() error        { a.killSwitches++; return nil }

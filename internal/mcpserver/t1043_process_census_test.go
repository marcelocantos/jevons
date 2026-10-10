// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// This is a process-free fixture, not a claim that 100 provider processes were
// launched. It tests the admission/routing seam with parked registry rows.
func TestT1043ParkedRowsDoNotConsumeAdmission(t *testing.T) {
	defs := make([]claudia.AgentDef, 0, 230)
	states := make(map[string]seatstate.Tri)
	for i := 0; i < 130; i++ {
		n := fmt.Sprintf("parked-%d", i)
		defs = append(defs, claudia.AgentDef{Name: n, Provider: claudia.ProviderClaude})
		states[n] = seatstate.No
	}
	for i := 0; i < 99; i++ {
		n := fmt.Sprintf("running-%d", i)
		defs = append(defs, claudia.AgentDef{Name: n, Provider: claudia.ProviderClaude})
		states[n] = seatstate.Yes // includes processes that have not submitted a turn
	}
	load := func() map[string]int {
		return map[string]int(activeProcessLoad(defs, func(n string) seatstate.Tri { return states[n] }))
	}
	snapshot := func() capacity.Snapshot {
		return CapacitySnapshot(CapacitySnapshotArgs{
			Budget:           func() *cost.BudgetConfig { return &cost.BudgetConfig{MaxSessions: 100} },
			Cost:             func() (*cost.Snapshot, error) { return &cost.Snapshot{Sessions: make([]cost.BurnRow, 210)}, nil },
			ProviderLoad:     load,
			ProviderSoftCaps: func() map[string]int { return map[string]int{"claude": 110, "grok": 12} },
		})
	}
	snap := snapshot()
	if snap.ActiveSessions != 99 || snap.CostWindowSessions != 210 || snap.ProviderLoad["claude"] != 99 {
		t.Fatalf("census %+v", snap)
	}
	if d := capacity.AdmitSpawnDest(capacity.SpawnWorker, "claude", snap, capacity.DefaultPolicy()); !d.Admitted() {
		t.Fatalf("99 live + 130 parked refused: %+v", d)
	}
	states["parked-0"] = seatstate.Yes
	snap = snapshot()
	if snap.ActiveSessions != 100 {
		t.Fatalf("at limit: %+v", snap)
	}
	if d := capacity.AdmitSpawnDest(capacity.SpawnWorker, "grok", snap, capacity.DefaultPolicy()); d.Admitted() {
		t.Fatalf("100 live admitted: %+v", d)
	}
	states["parked-0"] = seatstate.No
	// A provider cap must still bind even with global headroom. Give the
	// other 87 live seats a different provider and set claude's soft cap to 12.
	for i := 12; i < 99; i++ {
		defs[130+i].Provider = claudia.ProviderGrok
	}
	providerSnap := CapacitySnapshot(CapacitySnapshotArgs{
		Budget:           func() *cost.BudgetConfig { return &cost.BudgetConfig{MaxSessions: 100} },
		ProviderLoad:     load,
		ProviderSoftCaps: func() map[string]int { return map[string]int{"claude": 12, "grok": 100} },
	})
	if providerSnap.ActiveSessions != 99 || providerSnap.ProviderLoad["claude"] != 12 {
		t.Fatalf("provider fixture: %+v", providerSnap)
	}
	if d := capacity.AdmitSpawnDest(capacity.SpawnWorker, "claude", providerSnap, capacity.DefaultPolicy()); d.Admitted() {
		t.Fatalf("provider saturated despite global headroom: %+v", d)
	}
}

func TestT1043UnknownProcessFailsClosedButStoppedDoesNot(t *testing.T) {
	defs := []claudia.AgentDef{{Name: "unknown", Provider: claudia.ProviderGrok}, {Name: "stopped", Provider: claudia.ProviderGrok}}
	load := activeProcessLoad(defs, func(n string) seatstate.Tri {
		if n == "stopped" {
			return seatstate.No
		}
		return seatstate.Unknown
	})
	if load["grok"] != 1 {
		t.Fatalf("unknown must reserve a slot, stopped must not: %v", load)
	}
}

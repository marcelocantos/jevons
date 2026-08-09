// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cost"
)

// The adapter is the only place the cost/portfolio subsystems meet the pure
// admission policy, so its mapping is what keeps the policy honest.
func TestCapacitySnapshotCarriesBudgetLoadAndClampState(t *testing.T) {
	costSnap := &cost.Snapshot{
		Accounting:        cost.AccountingListPrice,
		Billable:          true,
		SpentTodayUSD:     120,
		ProjectedTodayUSD: 480,
		Sessions:          make([]cost.BurnRow, 7),
		Alerts: []cost.Alert{
			{Kind: cost.AlertProjection, Level: cost.LevelWarn},
			{Kind: cost.AlertFleetRate, Level: cost.LevelThrottle},
			{Kind: cost.AlertOrphanSessions, Level: cost.LevelWarn},
		},
	}
	budget := &cost.BudgetConfig{
		Accounting: cost.AccountingListPrice, DailyBudgetUSD: 500,
		HardCeilingUSDPerDay: 1500, MaxSessions: 20,
	}

	snap := CapacitySnapshot(CapacitySnapshotArgs{
		Cost:             func() (*cost.Snapshot, error) { return costSnap, nil },
		Budget:           func() *cost.BudgetConfig { return budget },
		TokensToday:      func() int64 { return 3_000_000 },
		DailyTokens:      func() int64 { return 10_000_000 },
		SpawnHalted:      func() bool { return false },
		OwnerActive:      func() bool { return true },
		ProviderLoad:     func() map[string]int { return map[string]int{"grok": 4} },
		ProviderSoftCaps: func() map[string]int { return map[string]int{"grok": 12} },
	})

	if snap.ActiveSessions != 7 || snap.MaxSessions != 20 {
		t.Errorf("load = %d/%d sessions, want 7/20", snap.ActiveSessions, snap.MaxSessions)
	}
	if snap.SpentTodayUSD != 120 || snap.ProjectedTodayUSD != 480 || snap.DailyBudgetUSD != 500 {
		t.Errorf("USD fields not carried: %+v", snap)
	}
	if snap.TokensUsed != 3_000_000 || snap.TokensBudget != 10_000_000 {
		t.Errorf("token fields not carried: %+v", snap)
	}
	// The alert that matters is the most severe one, not the last one seen.
	if snap.HighestAlert != "throttle" {
		t.Errorf("highest alert = %q, want throttle", snap.HighestAlert)
	}
	if !snap.OwnerActive || !snap.Billable {
		t.Errorf("owner_active/billable not carried: %+v", snap)
	}

	// End to end: throttle plus a 70%-spent token budget is tight, so only
	// load-bearing background survives.
	a := capacity.Assess(snap, capacity.DefaultPolicy())
	if a.Pressure != capacity.PressureTight {
		t.Fatalf("pressure = %s, want tight", a.Pressure)
	}
	if d := capacity.Admit(capacity.Request{Class: capacity.ClassResearch}, snap, nil); d.Admitted() {
		t.Errorf("research admitted under tight pressure: %+v", d)
	}
	if d := capacity.Admit(capacity.Request{Class: capacity.ClassOwnerTurn}, snap, nil); !d.Admitted() {
		t.Errorf("owner turn refused: %+v", d)
	}
}

// 🎯T137: a subscription snapshot must reach the policy labelled non-billable,
// or estimate dollars would start parking real work.
func TestCapacitySnapshotKeepsSubscriptionUnbillable(t *testing.T) {
	budget := &cost.BudgetConfig{Accounting: cost.AccountingSubscription}
	snap := CapacitySnapshot(CapacitySnapshotArgs{Budget: func() *cost.BudgetConfig { return budget }})
	if snap.Billable || snap.Accounting != cost.AccountingSubscription {
		t.Fatalf("snapshot = %+v, want subscription/non-billable", snap)
	}
}

// With no cost spine at all (usage.db unavailable, budget disabled) the
// snapshot must still be usable: unknown budget, slot-based admission.
func TestCapacitySnapshotWithoutCostSpineIsSlotBased(t *testing.T) {
	snap := CapacitySnapshot(CapacitySnapshotArgs{
		ProviderLoad:     func() map[string]int { return map[string]int{"grok": 1} },
		ProviderSoftCaps: func() map[string]int { return map[string]int{"grok": 12} },
	})
	a := capacity.Assess(snap, capacity.DefaultPolicy())
	if a.Pressure != capacity.PressureNormal {
		t.Fatalf("pressure = %s, want normal", a.Pressure)
	}
	if d := capacity.Admit(capacity.Request{Class: capacity.ClassResearch}, snap, nil); !d.Admitted() {
		t.Fatalf("research refused with no budget configured: %+v", d)
	}
}

// A nil governor must never wedge a loop: Ask falls through to admit.
func TestNilGovernorAdmitsSoLoopsKeepRunning(t *testing.T) {
	var s *Server
	d, release := capacity.Ask(s.CapacityGovernor(), capacity.ClassControlRepair, "boot")
	defer release()
	if !d.Admitted() {
		t.Fatalf("unwired governor refused work: %+v", d)
	}
}

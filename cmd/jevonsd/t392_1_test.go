// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/turnrate"
)

func TestTurnRateWiringDoesNotRemint(t *testing.T) {
	src, err := os.ReadFile("turnrate.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, bad := range []string{"PrepareCompaction", "compactFleetAgent", "CompactOverseer", "SeedSuccessor", ".rotate(", "Rewind"} {
		if strings.Contains(body, bad) {
			t.Fatalf("cmd/jevonsd/turnrate.go still remints via %s", bad)
		}
	}
	if !strings.Contains(body, "SetTurnGate") {
		t.Fatal("turn-rate wiring lost SetTurnGate")
	}
	mainSrc, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainSrc), "startTurnRate(guard, fleetAdapter)") {
		t.Fatal("main.go does not start the turn-rate governor")
	}
}

func TestPolicyFromBudgetMirrorsWorkerAndFleetLadders(t *testing.T) {
	cfg := cost.DefaultBudgetConfig()
	p := policyFromBudget(cfg)
	if p.ThrottleUSDPerHour != cfg.Worker.ThrottleUSDPerHour ||
		p.PauseUSDPerHour != cfg.Worker.PauseUSDPerHour ||
		p.RefuseUSDPerHour != cfg.Worker.KillUSDPerHour {
		t.Fatalf("worker ladder: %+v vs %+v", p, cfg.Worker)
	}
	if p.FleetThrottleUSDPerHour != cfg.Fleet.ThrottleUSDPerHour {
		t.Fatalf("fleet throttle=%v want %v", p.FleetThrottleUSDPerHour, cfg.Fleet.ThrottleUSDPerHour)
	}
	if p.MinSpacing != turnrate.DefaultMinSpacing {
		t.Fatalf("spacing=%s", p.MinSpacing)
	}
}

func TestBurnFromSnapMarksSubscription(t *testing.T) {
	snap := &cost.Snapshot{
		Accounting:       cost.AccountingSubscription,
		Billable:         false,
		FleetUSDPerHour:  40,
		WorkerUSDPerHour: map[string]float64{"w": 12},
	}
	b := burnFromSnap(snap, "w")
	if !b.Subscription || b.WorkerUSDPerHour != 12 || b.FleetUSDPerHour != 40 {
		t.Fatalf("%+v", b)
	}
	d := policyFromBudget(cost.DefaultBudgetConfig()).Admit(
		turnrate.Request{Agent: "w", SessionID: "s", LastTurn: snap.At}, b)
	if !d.Admitted() {
		t.Fatalf("subscription must not delay: %+v", d)
	}
}

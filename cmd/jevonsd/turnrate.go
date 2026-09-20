// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/turnrate"
)

// startTurnRate wires 🎯T392.1: burn is limited by when the host runs a
// turn (delay, pause, refuse), not by session size and not by minting a
// replacement. Nil guard (cost disabled / usage.db down) leaves the
// fleet ungated — T36 already logged that the cockpit is unguarded.
func startTurnRate(guard *costGuard, fleetAdapter *fleet.Claudia) {
	if guard == nil || fleetAdapter == nil {
		return
	}
	gov := turnrate.NewGovernor(turnrate.GovernorArgs{
		Policy: func() turnrate.Policy {
			return policyFromBudget(guard.config())
		},
		Burn: func(agent string) turnrate.Burn {
			snap, err := guard.monitor.Snapshot()
			if err != nil || snap == nil {
				return turnrate.Burn{}
			}
			return burnFromSnap(snap, agent)
		},
	})
	fleetAdapter.SetTurnGate(gov.AllowTurn)
	slog.Info("turn-rate governor",
		"lever", "delay/pause/refuse next turn",
		"remint", "withdrawn")
}

func policyFromBudget(cfg *cost.BudgetConfig) turnrate.Policy {
	if cfg == nil {
		return turnrate.DefaultPolicy()
	}
	return turnrate.Policy{
		ThrottleUSDPerHour:      cfg.Worker.ThrottleUSDPerHour,
		PauseUSDPerHour:         cfg.Worker.PauseUSDPerHour,
		RefuseUSDPerHour:        cfg.Worker.KillUSDPerHour,
		FleetThrottleUSDPerHour: cfg.Fleet.ThrottleUSDPerHour,
		FleetPauseUSDPerHour:    cfg.Fleet.PauseUSDPerHour,
		FleetRefuseUSDPerHour:   cfg.Fleet.KillUSDPerHour,
		MinSpacing:              turnrate.DefaultMinSpacing,
		Disabled:                cfg.Disabled,
	}
}

func burnFromSnap(snap *cost.Snapshot, agent string) turnrate.Burn {
	b := turnrate.Burn{
		FleetUSDPerHour: snap.FleetUSDPerHour,
		Subscription:    snap.Accounting == cost.AccountingSubscription || !snap.Billable,
	}
	if snap.WorkerUSDPerHour != nil {
		b.WorkerUSDPerHour = snap.WorkerUSDPerHour[agent]
	}
	return b
}

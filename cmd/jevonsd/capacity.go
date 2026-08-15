// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/hostload"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/server"
)

// capacityEventComponent is the eventlog component for admission notices.
const capacityEventComponent = "capacity"

// startCapacityGovernor wires background-work admission control (🎯T359):
// ambient cycles (research, coach, sentinel, audits) ask before each tick, and
// the answer comes from one holistic read of the remaining budget and the
// concurrent load rather than from each loop's own soft cap.
//
// It composes rather than replaces: the 🎯T36 clamp still owns enforcement,
// 🎯T137 accounting still decides whether USD may bind, and 🎯T325.2 soft caps
// supply the per-provider load headroom. With no cost spine (usage.db
// unavailable, or budget.json disabled) admission degrades to slot-based
// bounds — which is still better than every loop running blind.
func startCapacityGovernor(cfg config.Config, guard *costGuard, mcpSrv *mcpserver.Server, srv *server.Server) *capacity.Governor {
	path := capacity.ConfigPath(cfg.StateDir)
	pol, err := capacity.LoadPolicy(path)
	if err != nil {
		slog.Error("capacity: bad capacity.json — using defaults", "err", err, "path", path)
		pol = capacity.DefaultPolicy()
	}
	// Leave the owner a durable retune surface on first boot, the same way
	// the research and coach cycles do.
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if err := pol.Save(path); err != nil {
			slog.Warn("capacity: could not write default policy", "err", err, "path", path)
		}
	}

	args := mcpserver.CapacitySnapshotArgs{
		OwnerActive:      srv.OwnerTurnInFlight,
		ProviderLoad:     mcpSrv.HarnessLoad,
		ProviderSoftCaps: mcpSrv.EffectiveProviderSoftCaps,
		DailyTokens:      func() int64 { return pol.DailyTokenBudget },
		// The host itself, cached so a status call and an ambient tick in the
		// same second share one sysctl (🎯T463).
		HostLoad: hostload.Cached(0),
	}
	if guard != nil {
		args.Cost = guard.monitor.Snapshot
		args.Budget = guard.config
		args.SpawnHalted = func() bool { return guard.enforcer.AllowSpawn() != nil }
		args.TokensToday = func() int64 {
			now := time.Now()
			midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			tokens, err := guard.store.SpentTokens(midnight, now)
			if err != nil {
				slog.Warn("capacity: token total unavailable", "err", err)
				return 0
			}
			return tokens
		}
	}

	gov := capacity.NewGovernor(capacity.GovernorArgs{
		Snapshot: func() capacity.Snapshot { return mcpserver.CapacitySnapshot(args) },
		Policy:   func() *capacity.Policy { return pol },
		Notify: func(text string) {
			// Sticky, not chatty: the governor latches, so this fires once
			// when background parks and once when it resumes.
			slog.Warn("capacity notice", "msg", text)
			srv.Broadcast(map[string]any{"type": "capacity_notice", "message": text})
			srv.LogEvent(capacityEventComponent, "notice", map[string]any{"message": text})
			if err := srv.DeliverToOverseerAs(butler.FormatEventPush(capacityEventComponent, text), "agent"); err != nil {
				slog.Warn("capacity: overseer notice deliver failed", "err", err)
			}
		},
	})

	mcpSrv.SetCapacityGovernor(gov)
	srv.SetCapacitySource(func() any { return gov.Status() })

	tokenHint := "unset — set daily_token_budget in capacity.json to bind the token lever"
	if pol.DailyTokenBudget > 0 {
		tokenHint = "set"
	}
	slog.Info("capacity admission control ready (🎯T359)",
		"policy", path,
		"cost_spine", guard != nil,
		"daily_token_budget", tokenHint,
		"owner_reserve", pol.OwnerReserveFraction,
		"max_concurrent_background", pol.MaxConcurrentBackground,
		"api", "GET /api/capacity",
	)
	return gov
}

// capacityGate returns the governor as the loop-facing admission interface,
// or nil when admission control is not wired (loops then run ungated).
// A typed-nil *Governor inside a non-nil interface would defeat every loop's
// `gate == nil` check, so the conversion happens exactly here.
func capacityGate(gov *capacity.Governor) capacity.Gate {
	if gov == nil {
		return nil
	}
	return gov
}

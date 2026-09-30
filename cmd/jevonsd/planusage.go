// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/server"
)

// planAuthReviveInterval is how often a running seat on a plan is taken as
// evidence that its login works, reviving peers that broke on it.
const planAuthReviveInterval = 30 * time.Second

// startPlanUsage wires the subscription plan-usage reader (🎯T390): how much
// of each backend's allowance is left and when it rolls over, polled from
// claudia and served at GET /api/plan-usage for the cockpit.
//
// claudia 🎯T18 built the producer and nothing ever called it, so the owner
// ran dozens of agents with no view of the allowance they were spending. This
// is the consumer. It is deliberately unconditional — it does not hang off the
// cost guard, because the cost guard is switched off (budget.json
// disabled=true, owner, 2026-08-03) and plan remaining is exactly the number
// that stays meaningful when dollars do not.
func startPlanUsage(ctx context.Context, mcpSrv *mcpserver.Server, srv *server.Server, stateDir string, serveReady <-chan struct{}) *planusage.Reader {
	var hist planusage.History
	if path := planusage.DefaultReadingsPath(stateDir); path != "" {
		store, err := planusage.OpenReadingStore(path)
		if err != nil {
			slog.Warn("plan usage readings store", "err", err, "path", path)
		} else {
			hist = store
			go func() {
				<-ctx.Done()
				_ = store.Close()
			}()
		}
	}
	reader := planusage.NewReader(planusage.ReaderArgs{
		Load:     mcpSrv.HarnessLoad,
		History:  hist,
		OnUpdate: srv.FanPlanUsage,
	})
	// 🎯T948: the owner's band overrides ride the snapshot from its source,
	// so the bars, the sweep and the mint pick read one verdict.
	overrides := planusage.NewOverrideStore(filepath.Join(stateDir, planusage.OverrideFile))
	snapshot := func() planusage.Snapshot { return overrides.Apply(reader.Snapshot()) }
	overrides.OnChange(srv.FanPlanUsage)
	mcpSrv.SetPlanOverrides(overrides)
	srv.SetPlanUsageSource(func() any { return snapshot() })
	srv.SetPlanUsageWaitReady(reader.WaitReady)
	srv.SetPlanUsageRefresh(reader.RefreshNow)
	mcpSrv.SetPlanUsageSource(snapshot)
	srv.SetPlanSweep(func() any { return mcpSrv.SweepPlanPolicy() })
	srv.SetPlanDecisions(mcpSrv.PlanPolicyDecisions)
	srv.SetPlanRetryAfterReauth(mcpSrv.MarkPlanRetryAfterReauth)
	srv.SetPlanAuthRevive(func(provider claudia.Provider, skip string) {
		mcpSrv.RevivePlanAfterOwnerReauth(provider, skip) // 🎯T905
	})
	// A plan login repaired anywhere (one seat's Reauth, the CLI, another
	// host) shows up as a running seat on that plan; its auth-broken peers
	// then come back without another owner click.
	go func() {
		tick := time.NewTicker(planAuthReviveInterval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				attached := mcpSrv.ReattachRunningSeats()
				revived := mcpSrv.RevivePlanAuthWhereHealthy()
				// 🎯T943: a still-running seat refused on a revoked token does
				// not retry its own login — this is its half of the sweep, the
				// stopped-seat one is RevivePlanAuthWhereHealthy above.
				recoveredRunning := srv.RecoverRunningPlanAuthFailures(ctx)
				if len(revived) > 0 || len(attached) > 0 || len(recoveredRunning) > 0 {
					srv.NotifyAgentsChanged()
				}
			}
		}
	}()
	go reader.Run(ctx)
	// A daemon restart clears the in-memory execution result. Re-evaluate
	// after both the first plan reading and HTTP/MCP setup, so a rejected
	// destination login is visible again without waiting for the next tick.
	ready := make(chan struct{}, 1)
	go func() {
		if reader.WaitReady(ctx) != nil {
			return
		}
		select {
		case <-serveReady:
			ready <- struct{}{}
		case <-ctx.Done():
		}
	}()
	go func() {
		tick := time.NewTicker(planusage.DefaultRefresh)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ready:
				ready = nil // The first reading gets one sweep; later ticks continue it.
				mcpSrv.SweepPlanPolicy()
			case <-tick.C:
				mcpSrv.SweepPlanPolicy()
			}
		}
	}()

	slog.Info("plan usage ready (🎯T390)",
		"api", "GET /api/plan-usage",
		"refresh", planusage.DefaultRefresh,
		"stale_after", planusage.DefaultStaleAfter,
		"readings", planusage.DefaultReadingsPath(stateDir),
	)
	return reader
}

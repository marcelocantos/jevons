// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/server"
)

// planAuthReviveInterval is how often a running seat on a plan is taken as
// evidence that its login works, reviving peers that broke on it.
const planAuthReviveInterval = 30 * time.Second

// startPlanUsage subscribes to the one SQL projection on the existing
// control-plane replica. Claudia owns collection and its schedule.
func startPlanUsage(ctx context.Context, mcpSrv *mcpserver.Server, srv *server.Server, stateDir string, serveReady <-chan struct{}, replica *cost.ReplicaSource) (*planusage.Projection, error) {
	var projection *planusage.Projection
	updates := make(chan struct{}, 1)
	sourceReady := make(chan struct{})
	overrides, err := planusage.OpenReplicaOverrides(replica, stateDir)
	if err != nil {
		return nil, fmt.Errorf("plan usage replica setup: %w", err)
	}
	var burnMu sync.Mutex
	var burnProfiles map[string]planusage.BurnProfile
	var burnProfileAt time.Time
	projection, err = planusage.SubscribeProjection(ctx, replica, func() {
		// Browser delivery must not wait behind a migration/policy sweep.
		// Closing sourceReady publishes the registered source to this goroutine.
		select {
		case <-sourceReady:
			srv.FanPlanUsage()
		default:
		}
		select {
		case updates <- struct{}{}:
		default:
		}
	})
	if err != nil {
		return nil, fmt.Errorf("plan usage subscription: %w", err)
	}
	mcpSrv.SetPlanOverrides(overrides)
	// The local replica opens and subscribes even when the broker is absent.
	// Its link already retries with bounded backoff; do not mistake the empty
	// local projection for a connected (zero-usage) broker observation.
	srv.SetPlanUsageSource(func() any {
		burnMu.Lock()
		if time.Since(burnProfileAt) > 5*time.Minute {
			burnProfiles = planusage.MineBurnProfiles(replica)
			burnProfileAt = time.Now()
		}
		profiles := burnProfiles
		burnMu.Unlock()
		return servedPlanUsage(replica.LinkState().Linked, planusage.WithBurnProfilesJSON(projection.JSON(), profiles), projection.Snapshot().Backends)
	})
	close(sourceReady)
	mcpSrv.SetPlanUsageSource(projection.Snapshot)
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
				// 🎯T943: a still-running seat refused on a revoked token is
				// cleared once its plan is healthy again. It never refreshes or
				// signs in: that is the owner's, through Reauth (🎯T971).
				recoveredRunning := srv.RecoverRunningPlanAuthFailures(ctx)
				if len(revived) > 0 || len(attached) > 0 || len(recoveredRunning) > 0 {
					srv.NotifyAgentsChanged()
				}
			}
		}
	}()
	// Policies react to the same subscribed data after server setup. A
	// subscription notification is never a request to collect a provider.
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-serveReady:
		}
		srv.FanPlanUsage()
		mcpSrv.SweepPlanPolicy()
		mcpSrv.NoteCapacity()
		for {
			select {
			case <-ctx.Done():
				return
			case <-updates:
				mcpSrv.SweepPlanPolicy()
				mcpSrv.NoteCapacity()
			}
		}
	}()
	// A newly linked broker can have no plan rows yet. Publish the link
	// transition itself, not just subsequent changesets, to mux watchers.
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		linked := replica.LinkState().Linked
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				now := replica.LinkState().Linked
				if now != linked {
					linked = now
					srv.FanPlanUsage()
				}
			}
		}
	}()
	slog.Info("plan usage subscribed", "api", "GET /api/plan-usage", "source", "control-plane replica")
	return projection, nil
}

// planUsageDisconnected is served while the broker link is down and the
// replica has never observed a provider: an empty projection is not zero usage.
var planUsageDisconnected = json.RawMessage(`{"disabled":true,"error":"plan usage unavailable: control plane disconnected"}`)

// servedPlanUsage keeps last-good readings through a link drop. They render
// neutral and age by their own timestamps; replacing them with "disconnected"
// flipped every bar on each broker restart. Only a replica with no
// observation at all (🎯T1228's boot without a broker) reports disconnected.
func servedPlanUsage(linked bool, raw json.RawMessage, backends []planusage.Backend) json.RawMessage {
	if linked {
		return raw
	}
	for _, be := range backends {
		if !be.FetchedAt.IsZero() {
			return raw
		}
	}
	return planUsageDisconnected
}

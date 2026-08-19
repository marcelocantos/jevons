// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/ctxcap"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/server"
)

// Context ceiling governor (🎯T392.1 observation / 🎯T40.2 remint withdrawn /
// 🎯T417 unworkable report).
//
// Observes every registered agent's live context from the provider's own
// usage frames. A conversation that has grown large is NOT rotated onto a
// fresh session: remint is not how a conversation continues and not how
// burn is controlled. When it stays above the ceiling, the daemon reports
// the agent unworkable to its parent and the overseer — once per spell
// above the ceiling — naming context size, ceiling, and compaction cadence,
// instead of recompacting on a loop.

const ctxCapInterval = 2 * time.Minute

// UnworkableReporter delivers a 🎯T417 notice for one agent. parent may be
// empty; the reporter resolves lineage. Nil disables the notice (tests).
type UnworkableReporter func(agent, parent, text string)

// startContextCeiling launches the governor. Returns immediately; the loop
// runs until ctx is cancelled.
func startContextCeiling(ctx context.Context, cfg config.Config, roots discovery.Roots,
	reg *claudia.Registry, fleetAdapter *fleet.Claudia, srv *server.Server,
	rotations *handover.RotationStore, report UnworkableReporter) {
	pol := ctxcap.Policy{
		Ceiling:  cfg.ContextCeilingTokens,
		Disabled: cfg.ContextCeilingDisabled,
	}
	obs := ctxcap.Observer{Roots: roots}
	lastCompaction := map[string]time.Time{}
	var reported sync.Map // agent → struct{} sticky latch while above ceiling
	slog.Info("context ceiling governor",
		"ceiling", pol.EffectiveCeiling(), "disabled", pol.Disabled,
		"interval", ctxCapInterval, "remint", "withdrawn",
		"above_ceiling", "report_unworkable")

	go func() {
		t := time.NewTicker(ctxCapInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				contextCeilingPass(cfg, pol, obs, reg, fleetAdapter, srv, lastCompaction, rotations, report, &reported)
			}
		}
	}()
}

// contextCeilingPass evaluates the fleet once. It does not mint or seed.
func contextCeilingPass(cfg config.Config, pol ctxcap.Policy, obs ctxcap.Observer,
	reg *claudia.Registry, fleetAdapter *fleet.Claudia, srv *server.Server,
	lastCompaction map[string]time.Time, rotations *handover.RotationStore,
	report UnworkableReporter, reported *sync.Map) {
	if reg == nil {
		return
	}
	_ = fleetAdapter
	_ = srv
	_ = cfg
	for _, def := range reg.List() {
		if reg.Get(def.Name) == nil {
			continue
		}
		ref := ctxcap.AgentRef{
			Name:      def.Name,
			Provider:  string(def.Provider),
			WorkDir:   def.WorkDir,
			SessionID: def.SessionID,
		}
		o := obs.Observe(ref)
		o.Now = time.Now()
		o.SeedOnly = obs.SeedOnly(ref)
		if last, ok := lastCompaction[def.Name]; ok {
			o.SinceLastCompaction = o.Now.Sub(last)
		}
		since, ok := rotations.Observe(def.Name, o.Now)
		o = ctxcap.ApplyPersistedRotation(o, since, ok)
		d := pol.Evaluate(o)
		act := ctxcap.ActionFor(d)
		if act != ctxcap.ActionUnworkable {
			// Dropped under the ceiling (or unmeasured/exempt): clear the
			// sticky latch so a later climb can notify again.
			reported.Delete(def.Name)
			continue
		}
		// Loud on purpose: a large conversation used to remint. That
		// path is withdrawn — stay in this session and surface unworkable.
		slog.Info("context large — not reminting; reporting unworkable",
			"agent", def.Name, "verdict", d.Verdict, "context", d.Context,
			"ceiling", d.Ceiling, "reason", d.Reason)

		if _, already := reported.LoadOrStore(def.Name, struct{}{}); already {
			continue
		}
		if report == nil {
			continue
		}
		sinceLast := ctxcap.RotationAgeForNotice(o)
		text := ctxcap.FormatUnworkableNotice(d, pol.EffectiveMinInterval(), sinceLast)
		report(def.Name, def.Parent, text)
	}
}

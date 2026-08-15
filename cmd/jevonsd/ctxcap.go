// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/ctxcap"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/server"
)

// Context ceiling governor (🎯T392.1 observation / 🎯T40.2 remint withdrawn).
//
// Observes every registered agent's live context from the provider's own
// usage frames. A conversation that has grown large is logged. It is
// never rotated onto a fresh session: remint is not how a conversation
// continues and not how burn is controlled.

const ctxCapInterval = 2 * time.Minute

// startContextCeiling launches the governor. Returns immediately; the loop
// runs until ctx is cancelled.
func startContextCeiling(ctx context.Context, cfg config.Config, roots discovery.Roots,
	reg *claudia.Registry, fleetAdapter *fleet.Claudia, srv *server.Server,
	rotations *handover.RotationStore) {
	pol := ctxcap.Policy{
		Ceiling:  cfg.ContextCeilingTokens,
		Disabled: cfg.ContextCeilingDisabled,
	}
	obs := ctxcap.Observer{Roots: roots}
	lastCompaction := map[string]time.Time{}
	slog.Info("context ceiling governor",
		"ceiling", pol.EffectiveCeiling(), "disabled", pol.Disabled,
		"interval", ctxCapInterval, "remint", "withdrawn")

	go func() {
		t := time.NewTicker(ctxCapInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				contextCeilingPass(cfg, pol, obs, reg, fleetAdapter, srv, lastCompaction, rotations)
			}
		}
	}()
}

// contextCeilingPass evaluates the fleet once. It does not mint or seed.
func contextCeilingPass(cfg config.Config, pol ctxcap.Policy, obs ctxcap.Observer,
	reg *claudia.Registry, fleetAdapter *fleet.Claudia, srv *server.Server,
	lastCompaction map[string]time.Time, rotations *handover.RotationStore) {
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
		if ctxcap.ActionFor(d) != ctxcap.ActionObserve {
			continue
		}
		// Loud on purpose: a large conversation used to remint. That
		// path is withdrawn — stay in this session.
		slog.Info("context large — not reminting",
			"agent", def.Name, "verdict", d.Verdict, "context", d.Context,
			"ceiling", d.Ceiling, "reason", d.Reason)
	}
}

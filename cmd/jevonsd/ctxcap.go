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
	"github.com/marcelocantos/jevons/internal/server"
	"github.com/marcelocantos/jevons/internal/thread"
)

// Context ceiling governor (🎯T392.1).
//
// Observes every registered agent's live context from the provider's own
// usage frames and rotates the ones over the ceiling onto a fresh session,
// handing each successor its predecessor's transcript.
//
// The check interval is deliberately unhurried. A ceiling is not a
// tripwire that must fire the instant it is crossed — an agent a little
// over budget for a minute costs a rounding error, while a governor that
// polls aggressively spends real tokens of its own doing nothing.

const ctxCapInterval = 2 * time.Minute

// startContextCeiling launches the governor. Returns immediately; the loop
// runs until ctx is cancelled.
func startContextCeiling(ctx context.Context, cfg config.Config, roots discovery.Roots,
	reg *claudia.Registry, fleetAdapter *fleet.Claudia, srv *server.Server) {
	pol := ctxcap.Policy{
		Ceiling:  cfg.ContextCeilingTokens,
		Disabled: cfg.ContextCeilingDisabled,
	}
	obs := ctxcap.Observer{Roots: roots}
	slog.Info("context ceiling governor",
		"ceiling", pol.EffectiveCeiling(), "disabled", pol.Disabled,
		"interval", ctxCapInterval)

	go func() {
		t := time.NewTicker(ctxCapInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				contextCeilingPass(cfg, pol, obs, reg, fleetAdapter, srv)
			}
		}
	}()
}

// contextCeilingPass evaluates the fleet once and compacts what is over.
func contextCeilingPass(cfg config.Config, pol ctxcap.Policy, obs ctxcap.Observer,
	reg *claudia.Registry, fleetAdapter *fleet.Claudia, srv *server.Server) {
	if reg == nil {
		return
	}
	for _, def := range reg.List() {
		// A stopped agent is carrying no context into anything. Rotating it
		// would only discard a session it may still resume from.
		if reg.Get(def.Name) == nil {
			continue
		}
		d := pol.Evaluate(obs.Observe(ctxcap.AgentRef{
			Name:      def.Name,
			Provider:  string(def.Provider),
			WorkDir:   def.WorkDir,
			SessionID: def.SessionID,
		}))
		if d.Verdict != ctxcap.VerdictCompact {
			continue
		}
		slog.Info("context ceiling exceeded — compacting",
			"agent", def.Name, "context", d.Context, "ceiling", d.Ceiling)

		var err error
		if def.Purpose == claudia.PurposeOverseer {
			// Owner chat is attached by the server, so the overseer's
			// rotation has to re-attach there or the cockpit goes quiet.
			_, err = srv.CompactOverseer(false)
		} else {
			err = compactFleetAgent(fleetAdapter, def.Name)
		}
		if err != nil {
			// Loud, not fatal: a compaction that cannot complete leaves the
			// agent running over budget, which is the status quo, not a new
			// failure. The handover (if the row rotated) stays on disk.
			slog.Error("context compaction failed",
				"agent", def.Name, "context", d.Context, "err", err)
			continue
		}
		_ = cfg // reserved: per-purpose ceilings
	}
}

// compactFleetAgent runs the rotate → launch → seed sequence, the same one
// jevons_agent_migrate uses, with the provider held constant.
func compactFleetAgent(f *fleet.Claudia, name string) error {
	pending, err := f.PrepareCompaction(name, false)
	if err != nil {
		return err
	}
	if err := f.Launch(&thread.Thread{ID: name}); err != nil {
		// The row is already rotated and the handover is on disk, so the
		// next successful launch still delivers the predecessor's history.
		return err
	}
	if _, _, err := f.SeedSuccessor(name); err != nil {
		return err
	}
	slog.Info("agent compacted", "agent", name, "detail", pending.Describe())
	return nil
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// FleetOwnerPaused reports whether the fleet-wide intent is blocked_owner
// (🎯T966 owner pause from the cockpit).
func (s *Server) FleetOwnerPaused() bool {
	if s == nil {
		return false
	}
	return s.fleetIntent().FleetState() == fleetintent.BlockedOwner
}

// PauseFleetOwner stands the whole fleet down: durable blocked_owner intent
// plus the OMP agentPauseGate when sidecar seats are live (🎯T966).
// Non-OMP backends (Cursor, Claude CLI, Codex, Grok, …) have no native
// pause gate; they are held by the daemon-owned 🎯T414 intent checks on
// spawn, deliver-start, and idle nudge instead. Residue: a backend that ever
// ignored those checks would need a named hold mechanism — none are known.
func (s *Server) PauseFleetOwner(actor, reason string) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "owner"
	}
	if strings.TrimSpace(reason) == "" {
		reason = "owner pause"
	}
	return s.SetFleetIntent(fleetintent.BlockedOwner, actor, reason)
}

// ResumeFleetOwner clears a fleet-wide owner pause.
func (s *Server) ResumeFleetOwner(actor, reason string) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "owner"
	}
	if strings.TrimSpace(reason) == "" {
		reason = "owner resume"
	}
	if s.fleetIntent().FleetState() != fleetintent.BlockedOwner {
		return fmt.Errorf("fleet is not paused on the owner (intent is %s)", fleetintent.Describe(s.fleetIntent().FleetState()))
	}
	return s.SetFleetIntent(fleetintent.Working, actor, reason)
}

// ReconcileOMPPauseOnBoot re-applies the OMP pause gate after a daemon restart
// when intent.json still reads blocked_owner (🎯T966 state survival).
func (s *Server) ReconcileOMPPauseOnBoot() {
	if s == nil || !s.FleetOwnerPaused() {
		return
	}
	_ = s.syncOMPPause(true)
}

func (s *Server) syncOMPPause(paused bool) error {
	if err := claudia.SetOMPAgentPause(paused); err != nil {
		slog.Warn("fleet pause: omp sidecar gate",
			"component", "fleet_pause", "paused", paused, "err", err)
		return err
	}
	return nil
}

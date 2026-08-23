// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T406: a provider hard-block is an explicit fleet-wide intent, not an
// absence of work. Detection rides the same provider_failure surfaces as
// 🎯T283/T455; clearance rides a successful call — never a timer and never
// an operator assertion alone.

const (
	hardBlockBy      = "product:hard_block"
	hardBlockClear   = "product:provider_ok"
	hardBlockKind    = "provider-hard-block"
	hardBlockSubject = "fleet"
)

// ObserveProviderFailure records a classified provider refusal. When the
// refusal is a hard block (spend wall, revoked key, account block), the
// fleet-wide intent becomes blocked_provider and every spawn/nudge/revive
// control declines naming that intent. Ordinary transient failures are a
// no-op here — they must not stand the fleet down.
func (s *Server) ObserveProviderFailure(class agenterr.Class, raw string) {
	if s == nil || !agenterr.HardBlock(class, raw) {
		return
	}
	reason := agenterr.HardBlockReason(class, raw)
	if reason == "" {
		reason = "provider hard-block"
	}
	snap := s.fleetIntent()
	already := snap.FleetState() == fleetintent.BlockedProvider
	if err := s.SetFleetIntent(fleetintent.BlockedProvider, hardBlockBy, reason); err != nil {
		slog.Warn("provider hard-block intent failed",
			"component", "hard_block", "err", err, "reason", reason)
		return
	}
	slog.Warn("provider hard-block entered",
		"component", "hard_block",
		"failure_class", class.String(),
		"reason", reason,
		"already", already,
		"raw", truncate(strings.TrimSpace(raw), 200),
	)
	if already {
		return
	}
	// Owner-visible the moment it is detected — only the owner can clear the
	// account, and the overseer may itself be the seat that just hit the wall.
	text := "Provider hard-block: " + reason +
		". Fleet intent is blocked_provider — nothing will spawn, nudge, revive, " +
		"repressure, or repair into the wall until a successful provider call " +
		"clears it. " + truncate(strings.TrimSpace(raw), 240)
	if n := s.ownerNotifier; n != nil {
		n.NotifyOwner(hardBlockSubject, hardBlockKind, text)
	}
}

// ObserveProviderOK clears a blocked_provider fleet intent when the provider
// accepts work again. Other fleet intents (parked, blocked_owner) are left
// alone — success is evidence about the provider, not a licence to unpark.
func (s *Server) ObserveProviderOK() {
	if s == nil {
		return
	}
	if s.fleetIntent().FleetState() != fleetintent.BlockedProvider {
		return
	}
	if err := s.SetFleetIntent(fleetintent.Working, hardBlockClear, "provider accepted work again"); err != nil {
		slog.Warn("provider hard-block clear failed",
			"component", "hard_block", "err", err)
		return
	}
	slog.Info("provider hard-block cleared",
		"component", "hard_block",
		"by", hardBlockClear,
	)
	if n := s.ownerNotifier; n != nil {
		n.NotifyOwner(hardBlockSubject, hardBlockKind,
			"Provider hard-block cleared — a call succeeded. Fleet intent is working again.")
	}
}

// FleetIntentJSON is the owner-visible hard-block / intent snapshot for the
// cockpit (GET /api/fleet-intent).
func (s *Server) FleetIntentJSON() map[string]any {
	snap := s.fleetIntent()
	fleet := snap.Fleet
	out := map[string]any{
		"fleet_state": string(snap.FleetState()),
		"summary":     snap.Summarize(),
		"hard_block":  snap.FleetState() == fleetintent.BlockedProvider,
	}
	if fleet.State != "" && fleet.State != fleetintent.Unknown {
		out["fleet"] = map[string]any{
			"state":  string(fleet.State),
			"by":     fleet.By,
			"reason": fleet.Reason,
			"at":     fleet.At,
		}
	}
	held := snap.NotWorking()
	if len(held) > 0 {
		agents := make(map[string]any, len(held))
		for _, name := range held {
			rec := snap.Agents[name]
			agents[name] = map[string]any{
				"state":  string(rec.State),
				"by":     rec.By,
				"reason": rec.Reason,
				"at":     rec.At,
			}
		}
		out["agents"] = agents
	}
	return out
}

// handleFleetIntentHTTP serves GET /api/fleet-intent (🎯T406 clause 5).
func (s *Server) handleFleetIntentHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.FleetIntentJSON())
}

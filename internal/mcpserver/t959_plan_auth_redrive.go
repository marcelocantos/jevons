// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"sort"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T959: when a running seat's turn died on a revoked plan token and the
// broker then reloads that plan's login, the seat must start a new provider
// turn on the repaired credential immediately — not sit idle until the
// impatience ladder nudges it minutes later.

// onPlanLoginRecoveredRedrive runs after ClearPlanAuthFailures cleared auth
// refusals for the named seats. While fleet intent is blocked_provider the
// hard-block auth probe (🎯T968) owns the one allowed provider call; otherwise
// every still-running cleared seat is re-driven.
func (s *Server) onPlanLoginRecoveredRedrive(cleared []string) {
	if s == nil || len(cleared) == 0 {
		return
	}
	if s.fleetIntent().FleetState() == fleetintent.BlockedProvider {
		return
	}
	names := append([]string(nil), cleared...)
	sort.Strings(names)
	for _, name := range names {
		if name == "" {
			continue
		}
		slog.Info("plan login recovered; re-driving refused turn",
			"component", "plan_auth", "seat", name)
		s.submitPlanAuthRedrive(name)
	}
}

// submitPlanAuthRedrive hands the running seat back to the broker with an
// empty submit so pending work from the refused turn is retried on the
// repaired login (same mechanism as 🎯T968's hard-block probe).
func (s *Server) submitPlanAuthRedrive(name string) {
	s.mu.Lock()
	fn := s.planAuthRedriveFn
	s.mu.Unlock()
	if fn != nil {
		go fn(name)
		return
	}
	s.submitPlanAuthRedriveDefault(name)
}

func (s *Server) submitPlanAuthRedriveDefault(name string) {
	proc, live := s.liveSender(name)
	if !live {
		slog.Warn("plan auth redrive skipped: seat not running",
			"component", "plan_auth", "seat", name)
		return
	}
	if s.registry != nil {
		if p := s.registry.Get(name); p == nil || s.seatState(name).Alive != seatstate.Yes {
			slog.Warn("plan auth redrive skipped: seat not alive",
				"component", "plan_auth", "seat", name)
			return
		}
	}
	err := readoptDeliver(name, proc, func(a agentSender) error {
		return sendWithRequestID(a, "", "")
	})
	if err != nil {
		slog.Info("plan auth redrive handoff",
			"component", "plan_auth", "seat", name, "err", err)
	}
}

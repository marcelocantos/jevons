// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T968: while fleet intent is blocked_provider for an auth refusal, a plan
// login recovery (🎯T943 / 🎯T905 ClearPlanAuthFailures) triggers exactly one
// provider call on a running seat so ObserveProviderOK can clear the block
// without a human send. Spend-wall blocks are left alone.

// authProviderHardBlockReason reports whether the stored fleet-wide block is
// the auth-refusal shape (not a spend wall or account block).
func authProviderHardBlockReason(reason string) bool {
	want := agenterr.HardBlockReason(agenterr.ClassAuth, "")
	return strings.TrimSpace(reason) == want
}

// onPlanLoginRecoveredForAuthHardBlock runs after ClearPlanAuthFailures.
func (s *Server) onPlanLoginRecoveredForAuthHardBlock(recovered []string) {
	if s == nil || len(recovered) == 0 {
		return
	}
	snap := s.fleetIntent()
	if snap.FleetState() != fleetintent.BlockedProvider {
		return
	}
	if !authProviderHardBlockReason(snap.Fleet.Reason) {
		return
	}
	s.mu.Lock()
	fn := s.authHardBlockProbeFn
	s.mu.Unlock()
	name := pickAuthHardBlockProbeSeat(s, recovered)
	if name == "" {
		slog.Warn("provider hard-block auth probe skipped: no running recovered seat",
			"component", "hard_block", "recovered", recovered)
		return
	}
	slog.Info("provider hard-block auth probe after plan login recovery",
		"component", "hard_block", "seat", name, "recovered", recovered)
	if fn == nil {
		fn = s.submitAuthHardBlockProbe
	}
	go fn(name)
}

// pickAuthHardBlockProbeSeat chooses one running seat from the recovery batch.
func pickAuthHardBlockProbeSeat(s *Server, recovered []string) string {
	if s == nil || len(recovered) == 0 {
		return ""
	}
	names := append([]string(nil), recovered...)
	sort.Strings(names)
	for _, name := range names {
		if name == "" {
			continue
		}
		if s.registry != nil {
			if p := s.registry.Get(name); p == nil || s.seatState(name).Alive != seatstate.Yes {
				continue
			}
		}
		return name
	}
	return ""
}

// submitAuthHardBlockProbe starts one provider turn on a running seat while
// fleet intent still says blocked_provider. It bypasses deliver intent gates
// (🎯T1066) on purpose: the probe is the successful call that clears the
// block (🎯T406). An empty submit re-drives a refused turn on the repaired
// login when the broker still holds pending work (🎯T959 shape).
func (s *Server) submitAuthHardBlockProbe(name string) {
	proc, live := s.liveSender(name)
	if !live {
		slog.Warn("provider hard-block auth probe skipped: seat not running",
			"component", "hard_block", "seat", name)
		return
	}
	err := readoptDeliver(name, proc, func(a agentSender) error {
		return sendWithRequestID(a, "", "")
	})
	if err != nil {
		slog.Info("provider hard-block auth probe handoff",
			"component", "hard_block", "seat", name, "err", err)
	}
}

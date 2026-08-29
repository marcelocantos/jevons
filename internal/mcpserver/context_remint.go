// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T561: the daemon's own answer to "how do I remint this blown seat?".
// Wired into the 🎯T417 unworkable notice (advice line) and into
// jevons_agent_migrate (refuses a cross-provider move off a provider that
// still has weekly remaining unless the owner asked).

// contextRemintPlan decides the remint mode for agent from its registry
// provider and the live plan-usage snapshot. ok is false when the agent
// is not registered or has no provider (nothing to decide).
func (s *Server) contextRemintPlan(agent string, ownerAsked bool) (planusage.RemintPlan, bool) {
	if s == nil || s.registry == nil {
		return planusage.RemintPlan{}, false
	}
	def := s.registry.Def(strings.TrimSpace(agent))
	if def == nil || strings.TrimSpace(string(def.Provider)) == "" {
		return planusage.RemintPlan{}, false
	}
	args := planusage.RemintArgs{
		SeatProvider: string(def.Provider),
		OwnerAsked:   ownerAsked,
		Thresholds:   planusage.DefaultThresholds(),
	}
	if snap, _, now, th, ok := s.planPolicyInputs(); ok {
		args.Now, args.Thresholds = now, th
		args.Backend, args.Known = planusage.CockpitSnapshot(snap).Backend(args.SeatProvider)
	}
	return planusage.ContextRemintPlan(args), true
}

// refuseContextRemintMigrate returns the refusal text when moving name to
// target would leave a provider that still has weekly remaining without
// the owner asking (🎯T561). Empty means the migrate may proceed. A
// same-provider target is left to PrepareMigration's own refusal.
func (s *Server) refuseContextRemintMigrate(name, target string, ownerAsked bool) string {
	plan, ok := s.contextRemintPlan(name, ownerAsked)
	if !ok || plan.Mode != planusage.RemintSameProvider {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(target), plan.Provider) {
		return ""
	}
	return "refused (🎯T561): " + plan.Advice(name) +
		" Pass owner_asked=true only when the owner asked for the move."
}

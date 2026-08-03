// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"
)

// LooksLikeFinishedWorkReport is true when a terminal agent response claims
// completion with acceptable evidence (oracle or accepted-risk). Used to
// auto-deregister finished work agents (🎯T165). Mid-turn status with a SHA
// or "go test" alone does not match — a completion claim is required so WIP
// progress does not reap the worker.
func LooksLikeFinishedWorkReport(report string) bool {
	s := strings.ToLower(strings.TrimSpace(report))
	if s == "" {
		return false
	}
	if !hasCompletionClaim(s) {
		return false
	}
	return hasOracleEvidence(s) || hasAcceptedRisk(s)
}

// DurableFleetAgent reports whether name must not be auto-reaped on done
// (overseer, asides, product owners). Deliberate stop without kill remains
// allowed for everyone (🎯T165 residual).
func DurableFleetAgent(name, purpose string, isOverseer func(string) bool) bool {
	if name == "" {
		return true
	}
	if isOverseer != nil && isOverseer(name) {
		return true
	}
	p := strings.TrimSpace(purpose)
	if p == "" {
		p = claudia.PurposeWork
	}
	if p == claudia.PurposeOverseer || p == claudia.PurposeAside {
		return true
	}
	// Standing POs: jevons-po, *-po, and the short "po" test/fixture name.
	lower := strings.ToLower(name)
	if lower == "po" || strings.HasSuffix(lower, "-po") {
		return true
	}
	return false
}

// ShouldAutoReapDoneWorkAgent decides whether a finished-work report should
// stop+Remove the agent from the live fleet registry (🎯T165).
// Refuses durable roles, unregistered names, agents with descendants, bare
// done without evidence, and non-work purposes.
func ShouldAutoReapDoneWorkAgent(reg *claudia.Registry, name, report string, isOverseer func(string) bool) (bool, string) {
	if reg == nil || name == "" {
		return false, "no_registry_or_name"
	}
	if !LooksLikeFinishedWorkReport(report) {
		return false, "not_finished_work_report"
	}
	def := reg.Def(name)
	if def == nil {
		return false, "not_registered"
	}
	if DurableFleetAgent(name, def.Purpose, isOverseer) {
		return false, "durable_role"
	}
	if len(reg.Descendants(name)) > 0 {
		return false, "has_descendants"
	}
	return true, "finished_work"
}

// ReapDoneWorkAgent stops and deregisters name when ShouldAutoReapDoneWorkAgent
// is true. Returns (true, nil) when the agent left the registry.
// No-ops (false, nil) when the report is not a finished-work reaping case.
func ReapDoneWorkAgent(reg *claudia.Registry, name, report string, isOverseer func(string) bool) (bool, error) {
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, name, report, isOverseer)
	if !ok {
		return false, nil
	}
	if err := killSubtree(reg, name); err != nil {
		return false, fmt.Errorf("reap done %q (%s): %w", name, reason, err)
	}
	return true, nil
}

// maybeReapDoneWorkAgent runs after a terminal worker turn is notified to the
// overseer: if the reply is a finished-work report, leave the fleet (🎯T165).
func (s *Server) maybeReapDoneWorkAgent(name, report string) {
	if s == nil || s.registry == nil || name == "" || report == "" {
		return
	}
	ok, reason := ShouldAutoReapDoneWorkAgent(s.registry, name, report, s.isOverseerAgent)
	if !ok {
		return
	}
	if err := killSubtree(s.registry, name); err != nil {
		slog.Warn("T165 auto-reap failed", "agent", name, "reason", reason, "err", err)
		s.logLifecycle(compAgentLifecycle, "reap_done", "error", map[string]any{
			"name": name, "reason": reason, "err": err.Error(),
		})
		return
	}
	s.logLifecycle(compAgentLifecycle, "reap_done", "ok", map[string]any{
		"name": name, "reason": reason,
	})
	slog.Info("auto-reaped finished work agent", "agent", name, "reason", reason)
}

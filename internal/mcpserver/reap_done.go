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
// completion. Used to auto-deregister finished work agents (🎯T165 / 🎯T195).
//
// 🎯T195: a completion claim alone is enough for reaping (hygiene) even when
// the report omits oracle markers — imperfect "done" must not leave zombie
// implementers. Overseer acceptance still requires oracle or accepted-risk
// under 🎯T31.1; reaping ≠ accepting the work.
// Mid-turn status with a SHA or "go test" alone does not match — a completion
// claim is required so WIP progress does not reap the worker.
func LooksLikeFinishedWorkReport(report string) bool {
	s := strings.ToLower(strings.TrimSpace(report))
	if s == "" {
		return false
	}
	return hasCompletionClaim(s)
}

// finishedWorkReapReason classifies why a finished-work report is reaping.
func finishedWorkReapReason(report string) string {
	switch ClassifyCompletionReport(report) {
	case CompletionAcceptedRisk:
		return "accepted_risk"
	case CompletionOracleEvidence:
		return "finished_work"
	case CompletionBareDone:
		return "bare_done_completion" // 🎯T195 imperfect report
	default:
		return "finished_work"
	}
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
// stop+Remove the agent from the live fleet registry (🎯T165 / 🎯T195).
// Refuses durable roles, unregistered names, agents with descendants, and
// reports with no completion claim. Bare done (no oracle markers) still reaps
// work leaves — imperfect implementer reports must not leave zombies (🎯T195).
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
	return true, finishedWorkReapReason(report)
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
// overseer: if the reply is a finished-work report (including imperfect bare
// done — 🎯T195), leave the fleet (🎯T165).
func (s *Server) maybeReapDoneWorkAgent(name, report string) {
	if s == nil || s.registry == nil || name == "" || report == "" {
		return
	}
	ok, reason := ShouldAutoReapDoneWorkAgent(s.registry, name, report, s.isOverseerAgent)
	if !ok {
		return
	}
	if err := killSubtree(s.registry, name); err != nil {
		slog.Warn("T165/T195 auto-reap failed", "agent", name, "reason", reason, "err", err)
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

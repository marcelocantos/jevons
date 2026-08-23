// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/handover"
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
//
// 🎯T395: a completion word is necessary but not sufficient. A report that asks
// the overseer for a decision, states it has not acted yet, or arrives visibly
// cut is not a finish however confidently the rest of it reads — see
// ClassifyReportAsk. The veto is one-way on purpose: reaping is destructive and
// irreversible, keeping is cheap, so ambiguity resolves toward keeping.
//
// 🎯T445: the ask classes are phrase lists, so the completion word cannot be
// the whole positive case either — an ask worded outside every list must not
// fall through to a reap. The claim has to arrive in a positive finish shape
// (a bare claim clause, or a claim with oracle evidence or accepted-risk) —
// see hasFinishShape.
func LooksLikeFinishedWorkReport(report string) bool {
	if m, err := envelope.Parse(report); m != nil && err == nil {
		switch m.Kind {
		case envelope.KindFinishReport:
			return !ReportAwaitsOverseer(report)
		case envelope.KindStatusPing, envelope.KindAck, envelope.KindSpawnBrief, envelope.KindTargetFileRequest, envelope.KindScoutReport, envelope.KindEscalation:
			// 🎯T536.3: scout-report is a fog handoff, not product-done.
			return false
		}
	}
	s := strings.ToLower(strings.TrimSpace(report))
	if s == "" {
		return false
	}
	if !hasFinishShape(s) {
		return false
	}
	return !ReportAwaitsOverseer(report)
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
	// 🎯T395 before the generic no-claim case: a report that asks for a decision
	// is the opposite of a completion claim, and the lifecycle log should say so
	// rather than lumping it in with ordinary mid-turn chatter.
	if ask := ClassifyReportAsk(report); ask != AskNone {
		return false, "awaits_overseer_" + ask.String()
	}
	if !LooksLikeFinishedWorkReport(report) {
		return false, "not_finished_work_report"
	}
	// 🎯T470: a report the false-green check flagged is never auto-reaped as
	// finished_work in the same pass — jv-t391 was flagged
	// attestation_not_green and reaped finished_work 447ms later.
	if flags := FalseGreenFlags(report); len(flags) > 0 {
		return false, "false_green_" + string(flags[0].Kind)
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
// 🎯T435: acct accounts for the removal in the event log, naming the
// classifier that fired. A nil acct still reaps (slog-only).
func ReapDoneWorkAgent(reg *claudia.Registry, acct *fleetlog.Account, name, report string, isOverseer func(string) bool) (bool, error) {
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, name, report, isOverseer)
	if !ok {
		return false, nil
	}
	if err := killSubtree(reg, acct, name, reapDoneRemoval(reason)); err != nil {
		return false, fmt.Errorf("reap done %q (%s): %w", name, reason, err)
	}
	return true, nil
}

// reapDoneRemoval is the accounted cause for a 🎯T165/🎯T195 hygiene reap:
// the row went away because this agent said it had finished, and the
// classifier that read it that way is named so the reader can disagree.
func reapDoneRemoval(reason string) fleetlog.Removal {
	return fleetlog.Removal{
		Reason: fleetlog.ReasonReapDone,
		Detail: fmt.Sprintf("reaped on a finished-work report (%s)", reason),
		Fields: map[string]any{"reap_reason": reason},
	}
}

// maybeReapDoneWorkAgent runs after a terminal worker turn is notified to the
// overseer: if the reply is a finished-work report (including imperfect bare
// done — 🎯T195), leave the fleet (🎯T165).
//
// 🎯T471: a turn that ended because the 🎯T392.4 depth-ceiling ask told the
// worker to checkpoint must not auto-reap, even when the report text looks
// like a finish. EndTurn clears the counter's Requested flag before this
// runs, so observeTurnDepth latches checkpointEnded; we consume it here and
// KEEP the seat registered for resume. Ambiguity resolves toward keeping:
// a stranded live agent is recoverable; a deregistered one with uncommitted
// work in the shared tree is the 🎯T466 pile. A clear finish on a later turn
// that did not hit the ceiling still reaps under 🎯T165 / 🎯T195.
func (s *Server) maybeReapDoneWorkAgent(name, report string) {
	if s == nil || s.registry == nil || name == "" {
		return
	}
	if s.consumeCheckpointEnded(name) {
		fields := reapDecisionFields(name, "depth_ceiling_checkpoint", report)
		s.logLifecycle(compAgentLifecycle, "reap_done", "skipped", fields)
		slog.Info("T471 kept agent after depth-ceiling checkpoint turn",
			"agent", name, "report_len", len(report))
		return
	}
	// 🎯T474: reap_done must not Remove a name mid rotate→launch→seed.
	// An undelivered handover holds the seat; removing it lets the next
	// bare-thread Launch invent purpose=aside and discard the prepared
	// successor session (the jv-t444-phase-remint ghost).
	if s.rotationBlocksReap(name) {
		fields := reapDecisionFields(name, "rotation_pending", report)
		s.logLifecycle(compAgentLifecycle, "reap_done", "skipped", fields)
		slog.Info("T474 deferred finished-work reap while rotation pending",
			"agent", name)
		return
	}
	if report == "" {
		return
	}
	ok, reason := ShouldAutoReapDoneWorkAgent(s.registry, name, report, s.isOverseerAgent)
	if !ok {
		// 🎯T395 near-miss: the report carried completion language and would
		// have reaped under the old classifier. Log the save so the veto is
		// visible in the lifecycle stream rather than silently absent.
		if strings.HasPrefix(reason, "awaits_overseer_") &&
			hasCompletionClaim(strings.ToLower(report)) {
			s.logLifecycle(compAgentLifecycle, "reap_done", "skipped",
				reapDecisionFields(name, reason, report))
			slog.Info("T395 kept agent that asked rather than finished",
				"agent", name, "reason", reason)
		}
		// 🎯T470: same visibility for a false-green veto — the daemon held
		// that the evidence did not support the claim and must not also
		// treat the claim as finished_work.
		if strings.HasPrefix(reason, "false_green_") {
			fields := reapDecisionFields(name, reason, report)
			fields["false_green_flags"] = falseGreenKinds(FalseGreenFlags(report))
			s.logLifecycle(compAgentLifecycle, "reap_done", "skipped", fields)
			slog.Info("T470 kept agent whose finish report was false-green flagged",
				"agent", name, "reason", reason)
		}
		return
	}
	if err := killSubtree(s.registry, s.RemovalAccount(), name, reapDoneRemoval(reason)); err != nil {
		slog.Warn("T165/T195 auto-reap failed", "agent", name, "reason", reason, "err", err)
		fields := reapDecisionFields(name, reason, report)
		fields["err"] = err.Error()
		s.logLifecycle(compAgentLifecycle, "reap_done", "error", fields)
		return
	}
	s.logLifecycle(compAgentLifecycle, "reap_done", "ok",
		reapDecisionFields(name, reason, report))
	slog.Info("auto-reaped finished work agent", "agent", name, "reason", reason)
}

// reapDecisionFields is the lifecycle-log evidence for a reap decision (🎯T439):
// which classifier fired, on which phrase, and the span of report text around
// it. A reap removes an agent from the fleet irreversibly, and jv-t435 showed
// what the absence of this costs — a worker that had asked for its brief
// vanished as bare_done_completion, and the log recorded only that reason, so
// nothing in the record said which words had been read as a finish.
//
// The span reads differently either way round, and both are worth having: on a
// reap it is the completion phrase that fired, on a skip it is the ask that
// vetoed. Diagnostic only — nothing decides on these fields — so a missing span
// weakens a log line and never a decision.
func reapDecisionFields(name, reason, report string) map[string]any {
	fields := map[string]any{"name": name, "reason": reason}
	if ask := ClassifyReportAskDetail(report); ask.Class != AskNone {
		fields["ask_class"] = ask.Class.String()
		if ask.Marker != "" {
			fields["ask_marker"] = ask.Marker
		}
		fields["report_span"] = ask.Span
		fields["report_offset"] = ask.Offset
		return fields
	}
	if marker, span, offset, ok := FindCompletionClaim(report); ok {
		fields["claim_marker"] = marker
		fields["report_span"] = span
		fields["report_offset"] = offset
	}
	return fields
}

// rotationBlocksReap is true when an undelivered handover for name is on
// disk (🎯T474). The migrator may be unwired in hermetic tests — then
// nothing blocks.
func (s *Server) rotationBlocksReap(name string) bool {
	if s == nil || name == "" {
		return false
	}
	led, ok := s.migrator.(handoverLedger)
	if !ok || led == nil {
		return false
	}
	pending, err := led.PendingHandovers()
	if err != nil && len(pending) == 0 {
		return false
	}
	_, blocked := handover.FindBlockingRotation(pending, name)
	return blocked
}

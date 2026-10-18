// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/seatstop"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/reapverify"
)

// LooksLikeFinishedWorkReport is the supervision name of the one
// is-this-report-done answer (🎯T766.4). The reap path calls
// reportDoneForReap, which reads the same envelope.ClassifyDone. A report
// that declares no envelope kind is unknown, not finished. Reaping is not
// acceptance: a declared finish still owes oracle or accepted-risk under
// 🎯T31.1, and that check is the envelope's own validation.
func LooksLikeFinishedWorkReport(report string) bool {
	return envelope.IsDoneReport(report)
}

// reportDoneForReap is the reap path's only answer to is-this-report-done
// (🎯T766.4). Prose is not a fallback.
func reportDoneForReap(report string) (bool, string) {
	switch envelope.ClassifyDone(report) {
	case envelope.DoneYes:
		return true, ""
	case envelope.DoneNo:
		m, _ := envelope.Parse(report)
		if m != nil && m.Kind == envelope.KindFinishReport && m.Status == envelope.ProgressBlocked {
			return false, IdleSkipBlockedOnOwner
		}
		if m != nil && m.Kind == envelope.KindFinishReport && m.Status == envelope.ProgressInProgress {
			return false, outstandingScopeReapReason([]OutstandingScope{{
				Kind:   "in_progress",
				Detail: "the worker explicitly reports status in-progress",
			}})
		}
		return false, "not_finished_work_report"
	default:
		return false, "unknown_report"
	}
}

// finishedWorkReapReason names why a typed finish-report is reaping.
// The class comes from envelope slots. Payload prose is not consulted.
func finishedWorkReapReason(report string) string {
	m, err := envelope.Parse(report)
	if m == nil || err != nil || m.Kind != envelope.KindFinishReport {
		return "finished_work"
	}
	if m.HasRisk() {
		return "accepted_risk"
	}
	return "finished_work"
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

// ShouldAutoReapDoneWorkAgent decides whether a declared finish-report
// should stop+Remove the agent from the live fleet registry (🎯T165).
// Refuses durable roles, unregistered names, agents with descendants, and
// reports that do not declare a valid finish-report (🎯T766.4). Unenveloped
// prose is unknown, not finished.
func ShouldAutoReapDoneWorkAgent(reg *claudia.Registry, name, report string, isOverseer func(string) bool) (bool, string) {
	if reg == nil || name == "" {
		return false, "no_registry_or_name"
	}
	// 🎯T766.4: the done answer is the declared envelope kind. Unenveloped
	// prose is unknown, not a finish and not an ask-class reap.
	done, reason := reportDoneForReap(report)
	if !done {
		return false, reason
	}
	// 🎯T470: a report the false-green check flagged is never auto-reaped as
	// finished_work in the same pass — jv-t391 was flagged
	// attestation_not_green and reaped finished_work 447ms later.
	//
	// 🎯T946: the check also reads the worker's own worktree when one is
	// registered — a report that never cites a SHA or gate id but reads as
	// oracle evidence on marker words alone, over an uncommitted worktree,
	// is the same shape as a contradicted attestation (jv-t906-accept-lifts-block,
	// jv-t943-stale-token-reload).
	var workDir string
	if wd := reg.Def(name); wd != nil {
		workDir = wd.WorkDir
	}
	if flags := FalseGreenFlagsForWorker(report, workDir); len(flags) > 0 {
		return false, "false_green_" + string(flags[0].Kind)
	}
	def := reg.Def(name)
	if def == nil {
		return false, "not_registered"
	}
	if DurableFleetAgent(name, def.Purpose, isOverseer) {
		return false, "durable_role"
	}
	// 🎯T972: a finished-work report is not reaped while the seat's own
	// worktree holds uncommitted files the report never mentioned. A
	// sentence in the payload is not that fact (🎯T766.4). Checked after
	// false-green (a contradicted claim is the stronger veto) and before
	// the descendants check.
	if scope := outstandingScopeReasonsForWorker(report, workDir); len(scope) > 0 {
		return false, outstandingScopeReapReason(scope)
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
func (s *Server) maybeReapDoneWorkAgent(name, report string) {
	if s == nil || s.registry == nil || name == "" {
		return
	}
	if report == "" {
		return
	}
	var parent, targetID string
	if def := s.registry.Def(name); def != nil {
		parent = def.Parent
		targetID = def.TargetID
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
			var workDir string
			if def := s.registry.Def(name); def != nil {
				workDir = def.WorkDir
			}
			fields := reapDecisionFields(name, reason, report)
			fields["false_green_flags"] = falseGreenKinds(FalseGreenFlagsForWorker(report, workDir))
			s.logLifecycle(compAgentLifecycle, "reap_done", "skipped", fields)
			slog.Info("T470 kept agent whose finish report was false-green flagged",
				"agent", name, "reason", reason)
		}
		// 🎯T577: a checkpoint that the T165 path used to reap is kept;
		// name it so a skipped reap is attributable as that save, not
		// as a generic ask.
		// 🎯T581: a local done-word ("reading done; implementing now") is
		// a sub-step, not a finish — log the save so it is attributable.
		if reason == "not_finished_work_report" && allCompletionClaimsLocal(strings.ToLower(report)) {
			s.logLifecycle(compAgentLifecycle, "reap_done", "skipped",
				reapDecisionFields(name, "local_done_clause", report))
			slog.Info("T581 kept agent whose done-word was a local clause, not a finish",
				"agent", name)
		}
		// 🎯T750: a completion marker the worker cited rather than claimed.
		// Without this the save takes the silent return below and leaves no
		// record, which reads exactly like a sink that never ran.
		if reason == "not_finished_work_report" {
			if m, span, offset, cited := CitedCompletionClaim(report); cited {
				fields := map[string]any{
					"name":          name,
					"reason":        "quoted_completion_claim",
					"cited_marker":  m,
					"report_span":   span,
					"report_offset": offset,
				}
				s.logLifecycle(compAgentLifecycle, "reap_done", "skipped", fields)
				slog.Info("T750 kept agent whose completion marker was quoted, not claimed",
					"agent", name, "cited_marker", m)
			}
		}
		if strings.HasPrefix(reason, "awaits_overseer_checkpoint") ||
			(strings.HasPrefix(reason, "awaits_overseer_") && hasForwardLookingPlan(report)) {
			slog.Info("T577 kept agent whose report was a checkpoint, not a finish",
				"agent", name, "reason", reason)
		}
		// 🎯T972: a finished-work report was kept because it leaves scope
		// outstanding (a pending background gate, or uncommitted changes
		// owned in the seat's own worktree). The parent is notified by name
		// — a retained seat that nobody hears about is indistinguishable
		// from a stuck one.
		if strings.HasPrefix(reason, outstandingScopeReapReasonPrefix) {
			var workDir string
			if def := s.registry.Def(name); def != nil {
				workDir = def.WorkDir
			}
			scope := outstandingScopeReasonsForWorker(report, workDir)
			fields := reapDecisionFields(name, reason, report)
			var kinds []string
			for _, sc := range scope {
				kinds = append(kinds, sc.Kind)
			}
			fields["outstanding_scope"] = kinds
			s.logLifecycle(compAgentLifecycle, "reap_done", "skipped", fields)
			slog.Info("T972 kept agent with outstanding scope, not reaped",
				"agent", name, "reason", reason, "outstanding_scope", kinds)
			s.notifyParentOutstandingScope(parent, name, targetID, scope)
		}
		return
	}
	// 🎯T753: a target whose implementation already landed is held out of
	// the ready set until the parent decides achieve or reopen. The hold is
	// durable before the row goes; if it cannot be written the seat stays,
	// because a reap without it lets the sweep respawn onto landed work.
	pending, err := s.recordReapedTargetPending(name, reason)
	if err != nil {
		fields := reapDecisionFields(name, "reaped_target_pending_unrecorded", report)
		fields["err"] = err.Error()
		s.logLifecycle(compAgentLifecycle, "reap_done", "skipped", fields)
		slog.Warn("T753 kept finished agent: owed-decision record failed", "agent", name, "err", err)
		return
	}
	// 🎯T662: a reap is a recorded reason on the seat.
	s.noteSeatStop(name, seatstop.SourceReap, "reaped as finished work ("+reason+")", "daemon", "")
	// 🎯T738: stop host Goal continuation before the seat leaves the
	// registry. Once the row is gone claudia's askOwnerGoalCheck has no
	// owner to ask and reads that as "not complete", so a pane that
	// survived the kill is fed "Continue the open objective" forever.
	s.closeSeatGoalBeforeRemoval(name, "reaped_as_finished_work")
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
	if pending != nil {
		msg := reapverify.FormatOwedDecisionNotice(*pending)
		if _, err := s.deliverByName(pending.Owes, msg, OriginAgent, false); err != nil {
			slog.Warn("T753 owed-decision notice undelivered; escalating to overseer",
				"owes", pending.Owes, "worker", name, "target", pending.TargetID, "err", err)
			s.notifyFleetHealth(name, fmt.Sprintf("%s unreachable (%v) for: %s", pending.Owes, err, msg))
		}
	}

}

// FormatCheckpointReapRespawnNotice is the PO-facing 🎯T577 recovery: a
// worker was deregistered on a report that still named remaining work, so
// the leaf is not product-closed and needs a new seat. Phrased to stay on
// the PO (🎯T392.7 / 🎯T433): no oracle-done / needs-owner / blocked-on
// vocabulary.
func FormatCheckpointReapRespawnNotice(targetID, worker string) string {
	tid := FormatTargetID(targetID)
	if tid == "" {
		tid = "the open leaf"
	}
	w := strings.TrimSpace(worker)
	if w == "" {
		w = "worker"
	}
	return fmt.Sprintf("[checkpoint-reap 🎯T577] %s worker %s was deregistered on a report that still names remaining work. Respawn a worker for %s; the leaf is not product-closed.",
		tid, w, tid)
}

func (s *Server) notifyPORespawnAfterCheckpointReap(parent, worker, targetID string) {
	if s == nil || strings.TrimSpace(worker) == "" {
		return
	}
	po := strings.TrimSpace(parent)
	if po == "" {
		po = "jevons-po"
	}
	msg := FormatCheckpointReapRespawnNotice(targetID, worker)
	if _, err := s.deliverByName(po, msg, OriginAgent, false); err != nil {
		slog.Warn("T577 checkpoint-reap respawn notice undelivered to PO; escalating to overseer",
			"po", po, "worker", worker, "target", targetID, "err", err)
		s.notifyFleetHealth(worker, fmt.Sprintf("PO %s unreachable (%v) for: %s", po, err, msg))
	}
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

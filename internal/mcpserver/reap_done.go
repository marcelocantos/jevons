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
	"github.com/marcelocantos/jevons/internal/roles"
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
//
// A declared active external wait (🎯T1024, typed by 🎯T766.4.1) keeps the
// seat whatever the kind: on a finish-report it is the contradictory
// incident shape and names outstanding scope, so the parent is told; on a
// status-ping it is an ordinary mid-wait turn and keeps quietly.
func reportDoneForReap(report string) (bool, string) {
	if w, ok := declaredActiveWait(report); ok {
		if m, _ := envelope.Parse(report); m != nil && m.Kind == envelope.KindFinishReport {
			return false, outstandingScopeReapReason([]OutstandingScope{activeWaitScope(w)})
		}
		return false, activeWaitKeepReason
	}
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

// purposeProductOwner is the product-owner role token. Claudia's purpose
// is the work|aside|overseer enum, so in a live registry this arrives as a
// recorded role (agent_roles.json), not as AgentDef.Purpose. A seat name
// ending in -po is not this fact.
const purposeProductOwner = roles.ProductOwner

// DurableFleetAgent reports whether name must not be auto-reaped on done
// (overseer, asides, product owners). Deliberate stop without kill remains
// allowed for everyone (🎯T165 residual). role is the seat's declared role
// (Server.agentRole on the production path); a bare purpose reads as the
// role it implies. The name does not decide.
func DurableFleetAgent(name, role string, isOverseer func(string) bool) bool {
	if name == "" {
		return true
	}
	if isOverseer != nil && isOverseer(name) {
		return true
	}
	switch roles.OfSeat(role, "") {
	case roles.Overseer, roles.Aside, roles.ProductOwner:
		return true
	default:
		return false
	}
}

// durableSeat is ShouldAutoReapDoneWorkAgent's durable hook on the
// production path: the overseer, or a seat whose declared role is durable.
func (s *Server) durableSeat(name string) bool {
	if s.isOverseerAgent(name) {
		return true
	}
	purpose := ""
	if s.registry != nil {
		if d := s.registry.Def(name); d != nil {
			purpose = d.Purpose
		}
	}
	return DurableFleetAgent(name, s.agentRole(name, purpose), nil)
}

// ShouldAutoReapDoneWorkAgent decides whether a declared finish-report
// should stop+Remove the agent from the live fleet registry (🎯T165).
// Refuses durable roles, unregistered names, agents with descendants, and
// reports that do not declare a valid finish-report (🎯T766.4). Unenveloped
// prose is unknown, not finished.
//
// isDurable answers for seats the registry row alone cannot name: the
// overseer, and (on the production path, Server.durableSeat) a seat whose
// recorded role is durable.
func ShouldAutoReapDoneWorkAgent(reg *claudia.Registry, name, report string, isDurable func(string) bool) (bool, string) {
	if reg == nil || name == "" {
		return false, "no_registry_or_name"
	}
	// 🎯T766.4: the done answer is the declared envelope kind. Unenveloped
	// prose is unknown, not a finish and not an ask-class reap.
	done, reason := reportDoneForReap(report)
	if !done {
		return false, reason
	}
	// 🎯T470: a declared finish whose declared evidence is contradicted is
	// never auto-reaped as finished_work in the same pass — jv-t391 was
	// flagged attestation_not_green and reaped finished_work 447ms later.
	// 🎯T1027: nor one resting on another repo's gate. The veto reads typed
	// facts only (🎯T766.4.1): the envelope's slots and the gate record they
	// name. The prose rules of the report-time FALSE-GREEN banner are not
	// reachable from here.
	var workDir string
	if wd := reg.Def(name); wd != nil {
		workDir = wd.WorkDir
	}
	if flags := reapGateVeto(report, workDir); len(flags) > 0 {
		return false, "false_green_" + string(flags[0].Kind)
	}
	def := reg.Def(name)
	if def == nil {
		return false, "not_registered"
	}
	if DurableFleetAgent(name, roles.OfSeat(def.Role, def.Purpose), isDurable) {
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

// maybeReapStoredDoneWorkAgent re-reads the durable report store and runs the
// 🎯T165 path when the latest stored report classifies Done (🎯T1186). The
// send-path may store a finish-report while phase still reads turn_in_flight
// (worker-idle withheld before noteTurnEnded); once flight is idle, this is
// the second chance before idle-nudge would prod the seat.
func (s *Server) maybeReapStoredDoneWorkAgent(name string) bool {
	if s == nil || s.registry == nil || strings.TrimSpace(name) == "" {
		return false
	}
	if s.flightState(name) == FlightInFlight {
		return false
	}
	report := strings.TrimSpace(s.latestStoredReport(name))
	if report == "" || envelope.ClassifyDone(report) != envelope.DoneYes {
		return false
	}
	had := s.registry.Def(name) != nil
	s.maybeReapDoneWorkAgent(name, report)
	return had && s.registry.Def(name) == nil
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
	ok, reason := ShouldAutoReapDoneWorkAgent(s.registry, name, report, s.durableSeat)
	if !ok {
		// Skip logs repeat the reason ShouldAutoReapDoneWorkAgent already
		// returned. They match that string to the typed producer. They do
		// not scan the report for a completion word, an ask, or a prefix
		// of the reason (🎯T766.4).
		var workDir string
		if def := s.registry.Def(name); def != nil {
			workDir = def.WorkDir
		}
		if flags := reapGateVeto(report, workDir); len(flags) > 0 &&
			reason == "false_green_"+string(flags[0].Kind) {
			fields := reapDecisionFields(name, reason, report)
			fields["false_green_flags"] = falseGreenKinds(flags)
			s.logLifecycle(compAgentLifecycle, "reap_done", "skipped", fields)
			slog.Info("T470 kept agent whose finish report was false-green flagged",
				"agent", name, "reason", reason)
		}
		if scope := outstandingScopeReasonsForWorker(report, workDir); len(scope) > 0 &&
			reason == outstandingScopeReapReason(scope) {
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

// reapDecisionFields is the lifecycle-log evidence for a reap decision.
// The decision is the reason already returned and the envelope class
// ClassifyDone reads. A completion sentence in the payload is not a field
// (🎯T766.4). jv-t435 vanished on a prose span; the log now names the
// declared kind instead of the words that used to be read as a finish.
func reapDecisionFields(name, reason, report string) map[string]any {
	fields := map[string]any{
		"name":       name,
		"reason":     reason,
		"done_class": envelope.ClassifyDone(report).String(),
	}
	if m, _ := envelope.Parse(report); m != nil {
		fields["envelope_kind"] = string(m.Kind)
		if m.Status != envelope.ProgressNone {
			fields["envelope_status"] = string(m.Status)
		}
		if m.HasActiveWait() {
			fields["envelope_active_wait"] = m.ActiveWaits[0].String()
		}
	}
	return fields
}

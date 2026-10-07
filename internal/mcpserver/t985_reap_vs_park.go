// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// 🎯T985: a worker with no more work to do is reaped, not parked. Parking
// is reserved for a worker genuinely blocked on something external that
// may resume later.
//
// Owner directive 2026-10-02: "If a worker doesn't have any more work to do,
// it should be reaped rather than parked." Under the prior convention
// jevons-po used jevons_agent_stop (park, stay registered) when a worker's
// own work was complete or superseded — jv-t947-plan-token ("Nothing left
// for this worktree/worker.") and jv-t972-reap-scope ("T972 fully achieved
// and integrated ... worker has nothing further to do") were parked that
// way on 2026-09-30 and sat as standing stopped rows until the owner asked.
// That shape is now a reap (stop+Remove, the finished-work path in
// reap_done). Parking remains correct for a genuine external block:
// cross-repo work outside the seat's mandate (T947's claudia-side
// remainder, jv-t765.1-worker's bullseye-side work), an owner decision
// pending, or a design gate.
//
// Two paths consume the decision:
//   - handleAgentStop: a stop whose reason (or, when the reason is silent,
//     the seat's latest stored report) reads as own-work-complete is
//     converted to a reap. Durable roles and seats with descendants still
//     park — a reap of either is a kill, which has its own tool.
//   - ShouldAutoReapDoneWorkAgent: a terminal report that says "already
//     achieved by someone else, nothing left" is reaped as own_work_complete
//     even without a Done. finish shape (🎯T445), so the seat does not idle
//     until someone parks it.

// WorkerIdleAction is the lifecycle action 🎯T985 assigns to a worker's
// terminal/idle state.
type WorkerIdleAction string

const (
	// IdleActionKeep leaves the seat as it is: still working, or not
	// classifiable. handleAgentStop with this action still parks — that is
	// the explicit stop the caller asked for, not a T985 guess.
	IdleActionKeep WorkerIdleAction = "keep"
	// IdleActionReap is stop+Remove: own work complete or superseded,
	// nothing left for this seat.
	IdleActionReap WorkerIdleAction = "reap"
	// IdleActionPark is jevons_agent_stop staying registered: blocked on
	// something external that may resume.
	IdleActionPark WorkerIdleAction = "park"
)

// nothingLeftPhrases are the decisive "this seat has no more work" shapes.
// They outrank external-block vocabulary on purpose: the T947 park reason
// named a claudia-side remainder AND said "Nothing left for this
// worktree/worker" — the remainder was somebody else's, and this seat was
// finished. "no further acceptance" is the load-bearing acceptance case:
// a worker whose target was already achieved by someone else with no
// further acceptance criteria open.
var nothingLeftPhrases = []string{
	"nothing left",
	"nothing further",
	"nothing more to do",
	"no more work",
	"no further work",
	"no remaining work",
	"no further acceptance",
	"no work left",
	"no work remaining",
}

// completionPhrases are own-work-complete shapes that are not a Done.
// finish shape (🎯T445). They reap only when no external-block phrase is
// present: "jevons-side achieved; blocked on claudia-po for the rest" is a
// seat that still owns a remainder.
var completionPhrases = []string{
	"already achieved",
	"fully achieved",
	"already landed",
	"already integrated",
	"superseded",
	"duplicate of",
}

// blockedOnExternalPhrases are genuine external blocks that may resume.
// Distinct from own-work-complete: the worker still has a reason to exist
// once the external thing moves. The jv-t765.1-worker park reason is the
// specimen ("genuinely cross-repo blocked on bullseye's own src/apply.rs,
// which jevons-po has no mandate ... to touch; needs owner-level
// coordination").
var blockedOnExternalPhrases = []string{
	"blocked on",
	"blocked by",
	"cross-repo",
	"outside mandate",
	"outside its mandate",
	"outside my mandate",
	"no mandate",
	"claudia-side remainder",
	"bullseye-side",
	"owner decision",
	"owner go-ahead",
	"owner-level",
	"needs owner",
	"needs-owner",
	"design gate",
	"design-gated",
	"parked-for-design",
	"waiting on",
	"waiting for",
	"may resume",
	"resume later",
}

// ClassifyWorkerIdleDisposition classifies a worker's terminal/idle report
// (or a stop reason) into reap vs park vs keep (🎯T985).
//
// Order is load-bearing:
//  1. A typed status-blocked finish-report is park (🎯T938 — waiting on
//     someone else, may resume). Nothing in the prose outranks that.
//  2. An active wait on an alive external process (🎯T1024 — a queued
//     gate, a monitored background run) is park, unless a decisive
//     nothing-left phrase says the seat is finished regardless.
//  3. A typed finish-report, or a genuine finish shape, is reap (🎯T165).
//  4. A decisive nothing-left phrase is reap, even beside external-block
//     words (the remainder is someone else's).
//  5. External-block prose is park.
//  6. A completion phrase (already/fully achieved, superseded) is reap.
//  7. Otherwise keep.
//
// Quoted, cited, negated and fenced text is masked before the phrase scan
// (🎯T750): "the PO said 'nothing left' but I disagree" is not a claim.
func ClassifyWorkerIdleDisposition(text string) WorkerIdleAction {
	if strings.TrimSpace(text) == "" {
		return IdleActionKeep
	}
	if _, blocked := envelope.BlockedOn(text); blocked {
		return IdleActionPark
	}
	lower := claimScanText(strings.ToLower(text))
	// 🎯T1024: a seat narrating an active wait on an alive external process
	// (a queued gate on the shared lease, a monitored background run) is
	// blocked on something external that may resume — park, not reap —
	// whatever completion word a sub-step earned. A decisive nothing-left
	// phrase still outranks it, as it outranks every external-block shape.
	if declaresUnresolvedExternalWait(text) && !containsAny(lower, nothingLeftPhrases) {
		return IdleActionPark
	}
	if LooksLikeFinishedWorkReport(text) {
		return IdleActionReap
	}
	if containsAny(lower, nothingLeftPhrases) {
		return IdleActionReap
	}
	if containsAny(lower, blockedOnExternalPhrases) {
		return IdleActionPark
	}
	if containsAny(lower, completionPhrases) {
		return IdleActionReap
	}
	return IdleActionKeep
}

func containsAny(lower string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// LooksLikeOwnWorkCompleteReport is the auto-reap half of 🎯T985: a terminal
// report with no Done. finish shape that nonetheless says this seat has
// nothing left — its target was achieved by someone else, superseded, or
// has no further acceptance criteria open. It carries the same vetoes as
// LooksLikeFinishedWorkReport (an ask, a forward-looking plan, a report that
// awaits the overseer), because this is a new avenue into an irreversible
// reap and the 🎯T445 burden of proof applies to it too.
func LooksLikeOwnWorkCompleteReport(report string) bool {
	if strings.TrimSpace(report) == "" {
		return false
	}
	if _, blocked := envelope.BlockedOn(report); blocked {
		return false
	}
	if m, err := envelope.Parse(report); m != nil && err == nil && m.Kind != envelope.KindFinishReport {
		// A scout-report, status-ping, ack, escalation or brief is never
		// this seat's "nothing left".
		return false
	}
	if ClassifyReportAsk(report) != AskNone {
		return false
	}
	lower := claimScanText(strings.ToLower(report))
	if !containsAny(lower, nothingLeftPhrases) {
		return false
	}
	if hasForwardLookingPlan(report) {
		return false
	}
	// 🎯T581 shape: "nothing left on the scout; implementing now" closes a
	// sub-step, not the seat.
	if allNothingLeftClausesLocal(lower) {
		return false
	}
	return !ReportAwaitsOverseer(report)
}

// allNothingLeftClausesLocal is allCompletionClaimsLocal for the nothing-left
// phrases: true when every nothing-left sentence runs on into next-work
// language ("<verb>ing now", "now …", "next …") in the same sentence or the
// one after it.
func allNothingLeftClausesLocal(lower string) bool {
	sentences := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '\n'
	})
	claims := 0
	for i, sent := range sentences {
		if !containsAny(sent, nothingLeftPhrases) {
			continue
		}
		claims++
		clauses := strings.FieldsFunc(sent, finishClauseDelimiter)
		if i+1 < len(sentences) {
			clauses = append(clauses, strings.FieldsFunc(sentences[i+1], finishClauseDelimiter)...)
		}
		claimAt := -1
		for j, c := range clauses {
			if containsAny(c, nothingLeftPhrases) {
				claimAt = j
				break
			}
		}
		local := false
		for _, c := range clauses[claimAt+1:] {
			if nextWorkClause(c) {
				local = true
				break
			}
		}
		if !local {
			return false
		}
	}
	return claims > 0
}

// ownWorkCompleteReapReason is the ShouldAutoReapDoneWorkAgent reason for a
// 🎯T985 own-work-complete reap.
const ownWorkCompleteReapReason = "own_work_complete"

// StopDisposition is what handleAgentStop decided for one stop, and why.
type StopDisposition struct {
	Action WorkerIdleAction
	// Why is the operator-facing sentence: which input decided it.
	Why string
}

// stopDispositionArg is the optional jevons_agent_stop argument that fixes
// the disposition without classification.
const stopDispositionArg = "disposition"

// ClassifyStopDisposition is the pure decision behind handleAgentStop
// (🎯T985). explicit is the caller's disposition argument ("park" / "reap" /
// ""); reason is the stop reason; storedReport is the seat's latest stored
// report, consulted only when the reason is silent — the caller's stated
// reason outranks what the seat said earlier. durable and hasDescendants
// pin the answer to park: a reap of a PO, aside, overseer, or a seat with
// live children is a kill, which is a different tool. ownedUncommitted is
// 🎯T972's outstanding scope — a reap would lose it, so the seat parks and
// the caller is told.
func ClassifyStopDisposition(explicit, reason, storedReport string, durable, hasDescendants, ownedUncommitted bool) StopDisposition {
	explicit = strings.ToLower(strings.TrimSpace(explicit))
	switch {
	case durable:
		return StopDisposition{IdleActionPark, "durable role (PO / aside / overseer) — parked, not reaped; kill it with jevons_agent_kill if you mean that"}
	case hasDescendants:
		return StopDisposition{IdleActionPark, "has live descendants — parked, not reaped; kill the subtree with jevons_agent_kill subtree=true if you mean that"}
	case ownedUncommitted:
		return StopDisposition{IdleActionPark, "its own worktree holds uncommitted changes (🎯T972) — parked so they are not lost; commit or discard them, then stop again"}
	case explicit == string(IdleActionPark):
		return StopDisposition{IdleActionPark, "disposition=park"}
	case explicit == string(IdleActionReap):
		return StopDisposition{IdleActionReap, "disposition=reap"}
	}
	if a := ClassifyWorkerIdleDisposition(reason); a != IdleActionKeep {
		return StopDisposition{a, "the stop reason reads as " + describeIdleAction(a)}
	}
	if strings.TrimSpace(reason) == "" {
		if a := ClassifyWorkerIdleDisposition(storedReport); a != IdleActionKeep {
			return StopDisposition{a, "no reason given; the seat's latest stored report reads as " + describeIdleAction(a)}
		}
		return StopDisposition{IdleActionPark, "no reason given and nothing classifiable in the seat's latest report — parked"}
	}
	return StopDisposition{IdleActionPark, "the stop reason names neither finished work nor an external block — parked"}
}

func describeIdleAction(a WorkerIdleAction) string {
	switch a {
	case IdleActionReap:
		return "finished work (own work complete or superseded, nothing left for this seat)"
	case IdleActionPark:
		return "an external block that may resume"
	}
	return string(a)
}

// stopDispositionFor gathers the registry facts ClassifyStopDisposition
// needs for name.
func (s *Server) stopDispositionFor(name string, args map[string]any) StopDisposition {
	explicit, _ := args[stopDispositionArg].(string)
	reason, _ := args["reason"].(string)
	var def *claudia.AgentDef
	if s != nil && s.registry != nil {
		def = s.registry.Def(name)
	}
	if def == nil {
		// An unregistered name has nothing to reap; the park path reports it
		// as the pre-T985 code did.
		return StopDisposition{IdleActionPark, "not registered"}
	}
	durable := DurableFleetAgent(name, def.Purpose, s.isOverseerAgent)
	hasDescendants := len(s.registry.Descendants(name)) > 0
	ownedUncommitted := false
	for _, sc := range outstandingScopeReasonsForWorker("", def.WorkDir) {
		if sc.Kind == "owned_uncommitted" {
			ownedUncommitted = true
		}
	}
	return ClassifyStopDisposition(explicit, reason, s.latestStoredReport(name), durable, hasDescendants, ownedUncommitted)
}

// reapStopRemoval is the accounted cause for a 🎯T985 stop-to-reap: the row
// went away because a jevons_agent_stop said the seat's work was finished.
func reapStopRemoval(actor, why string) fleetlog.Removal {
	return fleetlog.Removal{
		Reason: fleetlog.ReasonReapStop,
		Detail: fmt.Sprintf("reaped on a jevons_agent_stop whose reason reads as finished work (%s)", why),
		Actor:  actor,
		Fields: map[string]any{"reap_reason": why},
	}
}

// FormatStopReapedResult is the jevons_agent_stop reply for a stop that
// became a reap (🎯T985).
func FormatStopReapedResult(name, why string) string {
	return fmt.Sprintf(
		"Agent %q stopped and reaped (removed from the registry, 🎯T985): %s. Parking is reserved for a seat blocked on something external that may resume later — to park instead, pass disposition=park and name the blocker in reason.",
		name, why)
}

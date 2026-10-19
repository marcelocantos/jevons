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
//     the seat's latest stored report) is a declared finish is converted
//     to a reap. Durable roles and seats with descendants still park — a
//     reap of either is a kill, which has its own tool.
//   - ShouldAutoReapDoneWorkAgent: a declared finish-report reaps. A
//     sentence that says the work is already achieved is unknown, not a
//     finish (🎯T766.4).

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

// ClassifyWorkerIdleDisposition classifies a worker's terminal/idle report
// (or a stop reason) into reap vs park vs keep (🎯T985).
//
// It reads the same done answer as reap (🎯T766.4). A declared
// finish-report reaps. A declared blocked finish-report parks. A report
// that declares nothing is keep: prose "already achieved" and "blocked on"
// are not a disposition.
func ClassifyWorkerIdleDisposition(text string) WorkerIdleAction {
	switch envelope.ClassifyDone(text) {
	case envelope.DoneYes:
		return IdleActionReap
	case envelope.DoneNo:
		m, err := envelope.Parse(text)
		if err == nil && m != nil && m.Kind == envelope.KindFinishReport && m.Status == envelope.ProgressBlocked {
			return IdleActionPark
		}
		return IdleActionKeep
	default:
		return IdleActionKeep
	}
}

// LooksLikeOwnWorkCompleteReport is the 🎯T985 name for a report whose own
// work is finished. It is ClassifyWorkerIdleDisposition's reap answer
// (🎯T766.4): a declared finish-report. A sentence that says nothing is
// left is not that claim.
func LooksLikeOwnWorkCompleteReport(report string) bool {
	return ClassifyWorkerIdleDisposition(report) == IdleActionReap
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
	durable := DurableFleetAgent(name, s.seatRoleOf(*def), s.isOverseerAgent)
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

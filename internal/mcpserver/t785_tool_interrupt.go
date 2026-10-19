// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/delivery"
)

// 🎯T785 — a fleet delivery never interrupts a seat's running tool call, and a
// seat never reads a fleet-caused interrupt as the owner's rejection.
//
// Specimens (2026-09-21): jv-t762 and jv-t763 both halted with Claude's
// "The user doesn't want to proceed … STOP" within ~1s of an automated fleet
// paste while a long Bash/bin/gate tool was mid-flight. The "rejected" command
// had already run; the seat then correctly refused later fleet nudges as
// not-the-owner and sat idle with a fresh transcript. Mechanism: a fleet path
// (resume interrupt=true, recover unstick, or a bare Send into Claude's client
// queue) cancelled the tool the way an owner Esc would.
//
// Rules:
//  1. While a tool_call_id is in progress and younger than the stuck bar, any
//     non-owner delivery is held the way client_queue holds a busy turn — the
//     running call is not cancelled.
//  2. When a fleet path does interrupt (tool past the stuck bar, or no tool
//     id known), the payload carries an in-band notice naming the fleet as
//     the interrupter so the seat does not invent an owner rejection.

// FleetToolInterruptNotice is prepended to a fleet payload that is about to
// cut a turn. The seat must treat the cut as fleet automation, not owner Esc.
const FleetToolInterruptNotice = "" +
	"[fleet interrupt 🎯T785] This turn was cut by the fleet daemon " +
	"(recover / resume / mode=interrupt from a non-owner path), NOT by the " +
	"owner rejecting a permission or tool. Any in-flight command may already " +
	"have finished with its own exit status. Do not STOP as if the owner " +
	"refused; read the message below and continue or report with evidence.\n\n"

// toolCallProgress reports the in-flight ACP tool_call_id for name, if any.
// since is age of that id (heartbeats do not reset it — 🎯T254.5.2).
func (s *Server) toolCallProgress(name string) (id string, since time.Duration, ok bool) {
	if s == nil {
		return "", 0, false
	}
	s.mu.Lock()
	tr := s.idleActivity
	s.mu.Unlock()
	if tr == nil {
		return "", 0, false
	}
	act := tr.Get(name)
	id = strings.TrimSpace(act.ToolCallID)
	if id == "" {
		return "", 0, false
	}
	if !act.ToolCallSince.IsZero() {
		since = tr.clock().Sub(act.ToolCallSince)
		if since < 0 {
			since = 0
		}
	}
	return id, since, true
}

// toolCallFresh reports a tool still inside the stuck bar — healthy long
// gates (bin/gate, multi-minute cargo/go tests) land here. A tool past the
// bar is fair game for fleet unstick (🎯T254.5.2 / 🎯T236).
func (s *Server) toolCallFresh(name string) bool {
	id, since, ok := s.toolCallProgress(name)
	if !ok || id == "" {
		return false
	}
	bar := DefaultFleetStuckTimeout
	return since < bar
}

// fleetInterruptSource is true when the delivery is not the owner speaking.
// Daemon-composed resumes and recoveries travel OriginAgent (or empty actor
// on the owner surface with OriginAgent); an owner mode=interrupt remains
// free to cut (🎯T711).
func fleetInterruptSource(origin SendOrigin) bool {
	return origin != OriginOwner
}

// demoteFleetInterruptForTool turns a fleet mode=interrupt into submit when
// the seat still has a fresh tool call. Returns the mode to run and whether
// the demotion applied.
func (s *Server) demoteFleetInterruptForTool(name string, origin SendOrigin, mode delivery.Mode) (delivery.Mode, bool) {
	if s == nil || !mode.Interrupts() || !fleetInterruptSource(origin) {
		return mode, false
	}
	if !s.toolCallFresh(name) {
		return mode, false
	}
	id, since, _ := s.toolCallProgress(name)
	slog.Info("🎯T785 fleet interrupt demoted — tool still running",
		"component", "agent_send",
		"name", name,
		"tool_call_id", id,
		"tool_since", since.String(),
		"origin", string(origin),
		"was_mode", string(mode),
	)
	return delivery.ModeSubmit, true
}

// protectToolInFlight queues a non-owner delivery when a tool is mid-flight,
// even if the turn flight ledger is idle/unknown (stale after a bounce). The
// running call is not cancelled. Returns a result when the protect arm ran.
func (s *Server) protectToolInFlight(name, text, requestID string, origin SendOrigin, mode delivery.Mode, rehydrated bool) (agentSendResult, bool, error) {
	if s == nil || !fleetInterruptSource(origin) {
		return agentSendResult{}, false, nil
	}
	// Owner-path interrupt after demotion refused is already ModeSubmit here;
	// ModeInterrupt that survived demotion means the tool is past the bar (or
	// absent) and the fleet is allowed to cut — do not re-queue it.
	if mode.Interrupts() {
		return agentSendResult{}, false, nil
	}
	if !s.toolCallFresh(name) {
		return agentSendResult{}, false, nil
	}
	// Already on the ordinary busy arm when flight is in_flight; this arm is
	// the tool-id reading that flightState may miss.
	if s.flightState(name) == FlightInFlight {
		return agentSendResult{}, false, nil
	}
	id, since, _ := s.toolCallProgress(name)
	n, qerr := s.enqueueAgentSendWithRequestID(name, text, requestID)
	if qerr != nil {
		return agentSendResult{}, true, undeliverableQueueError(name, qerr)
	}
	mm := sendMech{Mode: mode, Mechanism: delivery.MechanismClientQueue}
	msg := fmt.Sprintf(
		"busy: %q has tool %s in flight (since %s); message queued (%d pending) until the tool returns — "+
			"held by the daemon so the running call is not cancelled (🎯T785). "+
			"To cut only a stuck tool past the recover bar: jevons_agent_send with mode=interrupt after the tool ages out.",
		name, id, since.Round(time.Second), n)
	res := agentSendResult{
		Status:    "queued",
		Message:   msg + describeMode(mm),
		Queued:    n,
		Mode:      mode,
		Mechanism: mm.Mechanism,
	}
	logAgentSendOutcome(name, res, rehydrated, OutcomeQueuedBehindTurn, FlightUnknown, TurnEvidence{
		Detail: fmt.Sprintf("tool_call_id=%s since=%s (🎯T785)", id, since),
	})
	return res, true, nil
}

// withFleetInterruptNotice labels a fleet interrupt so the seat does not
// invent an owner rejection. Idempotent.
func withFleetInterruptNotice(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return FleetToolInterruptNotice
	}
	if strings.Contains(text, "[fleet interrupt 🎯T785]") {
		return text
	}
	return FleetToolInterruptNotice + text
}

// noteFleetInterruptIfNeeded wraps text when a non-owner interrupt is about
// to cut the seat. Owner Esc is left alone.
func noteFleetInterruptIfNeeded(origin SendOrigin, mode delivery.Mode, text string) string {
	if !mode.Interrupts() || !fleetInterruptSource(origin) {
		return text
	}
	return withFleetInterruptNotice(text)
}

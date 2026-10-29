// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// ambientFleetDelivery reports whether this send is an ambient agent-origin
// delivery (repo-watch, sentinel, coach, post-reap, idle nudge, …) rather
// than an owner-origin or overseer-direct send (🎯T1229).
//
// Owner-origin and overseer-direct keep their current behaviour: a live
// parked seat still goes through deliverToSenderMode (🎯T1066 may hold the
// idle submit), and a stopped parked seat still fails at ensureAgentProcess
// (🎯T408). Ambient deliveries must not start a model turn even when the
// process is already alive — and must not fall through to butler.PushEvent
// when ensureAgentProcess errors.
func (s *Server) ambientFleetDelivery(origin SendOrigin, actor string, rel DeliverRelation) bool {
	if origin != OriginAgent {
		return false
	}
	if rel == RelationDirectDown && actor != "" && s.isOverseerAgent(actor) {
		return false
	}
	return true
}

// holdAmbientIfUnrunnable parks repo-watch / sentinel / coach / post-reap
// (and other OriginAgent) payloads in sendq when fleet intent is not
// runnable, before ensureAgentProcess or butler.PushEvent can start a turn.
// Interrupt and steer are caller overrides and are left alone.
func (s *Server) holdAmbientIfUnrunnable(name, text, requestID string, origin SendOrigin, actor string, rel DeliverRelation, mode delivery.Mode) (agentSendResult, bool, error) {
	if !s.ambientFleetDelivery(origin, actor, rel) {
		return agentSendResult{}, false, nil
	}
	if mode.Interrupts() || mode == delivery.ModeSteer {
		return agentSendResult{}, false, nil
	}
	dec := s.AllowFleetControl(name, fleetintent.ControlDeliverSubmit)
	if dec.Allow {
		return agentSendResult{}, false, nil
	}
	n, qerr := s.enqueueAgentSendWithRequestID(name, text, requestID)
	if qerr != nil {
		return agentSendResult{}, true, undeliverableQueueError(name, qerr)
	}
	pending := s.pendingAgentSends(name)
	if pending < 0 {
		pending = n
	}
	msg := fmt.Sprintf(
		"agent %q is %s (%s) — ambient delivery held by the daemon (%d pending), NOT a model turn; "+
			"it drains once the intent is lifted (jevons_fleet_intent state=working) — 🎯T1229.",
		name, fleetintent.Describe(dec.Blocking), dec.Reason, pending)
	res := agentSendResult{
		Status:    "queued",
		Message:   msg,
		Queued:    pending,
		Mode:      mode,
		Mechanism: delivery.MechanismClientQueue,
	}
	if res.Mode == "" {
		res.Mode = delivery.ModeSubmit
	}
	logAgentSendOutcome(name, res, false, OutcomeQueuedBehindTurn, FlightIdle, TurnEvidence{
		Detail: "held ambient delivery; fleet intent not runnable (🎯T1229)",
	})
	slog.Info("ambient delivery held by fleet intent",
		"component", "agent_send", "name", name,
		"blocking", string(dec.Blocking), "reason", dec.Reason, "queued", pending)
	return res, true, nil
}

// noteParkedSeatOnSend appends a parked/blocked note to owner-origin and
// overseer-direct results so the send answer names the intent (🎯T1229).
func (s *Server) noteParkedSeatOnSend(name string, origin SendOrigin, actor string, rel DeliverRelation, res agentSendResult) agentSendResult {
	if s.ambientFleetDelivery(origin, actor, rel) {
		return res
	}
	dec := s.AllowFleetControl(name, fleetintent.ControlDeliverSubmit)
	if dec.Allow {
		return res
	}
	desc := fleetintent.Describe(dec.Blocking)
	lower := strings.ToLower(res.Message)
	if strings.Contains(lower, "parked") || strings.Contains(res.Message, desc) {
		return res
	}
	res.Message = strings.TrimSpace(res.Message) + " Seat is " + desc + "."
	return res
}

// holdAmbientInsteadOfPush is the butler.PushEvent fallback guard: a send
// error on a not-runnable seat must enqueue, never start a model turn.
// held is true when intent refused the turn (even if enqueue failed).
func (s *Server) holdAmbientInsteadOfPush(name, text string) (held bool, err error) {
	dec := s.AllowFleetControl(name, fleetintent.ControlDeliverSubmit)
	if dec.Allow {
		return false, nil
	}
	if _, err := s.enqueueAgentSend(name, text); err != nil {
		return true, err
	}
	slog.Info("ambient delivery held after send error",
		"component", "agent_send", "name", name,
		"blocking", string(dec.Blocking), "reason", dec.Reason)
	return true, nil
}

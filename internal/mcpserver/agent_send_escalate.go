// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/escalate"
)

// 🎯T899: a product owner that gets its own hands dirty must stay reachable.
// A message to an agent busy with its own turn is normally held until the
// turn ends; an owner message, or the overseer directing the agent below it,
// instead steers into the running turn at once and interrupts it if the
// agent has not taken the message by the profile's deadline. Claudia runs the
// ladder beside the seat (claudia 🎯T138); Jevons chooses it by sender.

// escalatingSender is a seat that can run an escalation ladder.
type escalatingSender interface {
	SendEscalating(string, claudia.Escalation) (claudia.DeliveryOutcome, error)
}

// SetDeliveryEscalation installs the urgency profile from config.
func (s *Server) SetDeliveryEscalation(c config.DeliveryEscalationConfig) {
	s.mu.Lock()
	s.deliveryEscalation = c
	s.mu.Unlock()
}

// escalationClass names the urgency class of one delivery, or "" for a
// sender whose messages wait for the turn boundary.
func (s *Server) escalationClass(actor string, origin SendOrigin, rel DeliverRelation) string {
	switch {
	case origin == OriginOwner:
		return config.EscalationOwner
	case origin == OriginAgent && rel == RelationDirectDown && actor != "" && s.isOverseerAgent(actor):
		return config.EscalationOverseer
	}
	return ""
}

// escalationLadder is the Claudia ladder for class, or ok=false.
// EscalationLadderFor is the configured ladder for an urgency class, for the
// owner-chat layer's overseer arm (🎯T903).
func (s *Server) EscalationLadderFor(class string) (claudia.Escalation, bool) {
	return s.escalationLadder(class)
}

func (s *Server) escalationLadder(class string) (claudia.Escalation, bool) {
	if class == "" {
		return nil, false
	}
	s.mu.Lock()
	cfg := s.deliveryEscalation
	s.mu.Unlock()
	first, after, ok := cfg.Ladder(class)
	if !ok {
		return nil, false
	}
	ladder := claudia.Escalation{{Mode: claudia.DeliveryMode(first)}}
	if after > 0 {
		ladder = append(ladder, claudia.EscalationStep{Mode: claudia.DeliveryInterrupt, After: after})
	}
	return ladder, true
}

// escalateIfBusy offers text to a busy seat under its sender's ladder.
// handled=false means the seat is idle, the sender has no ladder, or the
// seat cannot run one: the caller continues on the ordinary path, which
// submits to an idle seat and holds a message for a busy one.
func (s *Server) escalateIfBusy(name, text, class, asker string, proc agentSender) (agentSendResult, bool, error) {
	ladder, ok := s.escalationLadder(class)
	if !ok {
		return agentSendResult{}, false, nil
	}
	es, ok := proc.(escalatingSender)
	if !ok {
		return agentSendResult{}, false, nil
	}
	unlock := s.lockAgentSend(name)
	defer unlock()
	if proc == nil || !proc.Alive() || !(s.flightState(name) == FlightInFlight || senderTurnInFlight(proc)) {
		return agentSendResult{}, false, nil
	}
	// 🎯T931: the ladder holds only rungs this seat can run. A steer-first
	// ladder cannot start on a seat with no steer mechanism (Claude Code in
	// tmux), so the ordinary path holds the message for the turn boundary.
	if caps, ok := escalate.CapsOf(proc); ok {
		fitted := escalate.Fit(ladder, caps)
		if len(fitted) == 0 {
			slog.Info("🎯T931 seat cannot start the escalation ladder; ordinary delivery",
				"component", "agent_send", "name", name, "class", class,
				"steer_policy", string(caps.SteerPolicy))
			return agentSendResult{}, false, nil
		}
		ladder = fitted
	}
	// 🎯T902: the overseer (or any non-owner asker) hears the mid-turn
	// answer as soon as it is given; the owner reads it in the agent's pane.
	// Registered before the send, because the seat may take it at once.
	relay := class != config.EscalationOwner && asker != ""
	if relay {
		s.midTurn().expect(name, asker, text)
	}
	out, err := es.SendEscalating(text, ladder)
	if err != nil {
		if relay {
			s.midTurn().forget(name, text)
		}
		if escalate.NotStarted(err) {
			// Nothing reached the seat; the ordinary path holds or submits it.
			slog.Info("🎯T899 escalation not available on this seat; ordinary delivery",
				"component", "agent_send", "name", name, "class", class, "err", err.Error())
			return agentSendResult{}, false, nil
		}
		return agentSendResult{}, true, fmt.Errorf("escalating send to %q: %w", name, err)
	}
	status, verb := "steered", "steered into its running turn"
	if ladder[0].Mode == claudia.DeliverySubmit {
		status, verb = "sent", "queued with the agent behind its running turn"
	}
	msg := fmt.Sprintf("%q is busy: message %s (%s urgency", name, verb, class)
	if len(ladder) > 1 {
		msg += fmt.Sprintf("; the turn is interrupted after %s if it has not taken the message", ladder[1].After)
	}
	msg += ", 🎯T899)."
	slog.Info("agent_send",
		"component", "agent_send", "name", name, "status", status,
		"mode", string(ladder[0].Mode), "mechanism", out.Mechanism,
		"outcome", "escalating", "class", class, "ladder", len(ladder))
	res := agentSendResult{
		Status:    status,
		Message:   msg,
		Mode:      delivery.Mode(ladder[0].Mode),
		Mechanism: out.Mechanism,
	}
	if len(ladder) > 1 {
		res.InterruptAfter = ladder[1].After
	}
	return res, true, nil
}

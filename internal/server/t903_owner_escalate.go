// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"log/slog"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/escalate"
)

// 🎯T903: an owner message to an overseer that is mid-way through an owner
// turn used to wait in the notify queue until that turn ended — five minutes
// on 2026-09-29, until the owner pressed Steer by hand. Now it takes the
// owner escalation ladder, as a message to any agent does (🎯T899): steered
// into the running turn at once, and the turn interrupted after the bound if
// the overseer has not taken it. A fleet-note turn is still interrupted
// outright for the owner (🎯T291); that path is unchanged.

// SetOwnerEscalation installs the owner urgency ladder (config, via the fleet
// layer). A nil func or a false return leaves the queue-behind-turn path.
func (s *Server) SetOwnerEscalation(fn func() (claudia.Escalation, bool)) {
	s.mu.Lock()
	s.ownerEscalation = fn
	s.mu.Unlock()
}

// overseerEscalator is the part of the overseer's seat the ladder needs.
type overseerEscalator interface {
	Alive() bool
	TurnPhase() claudia.TurnPhase
	SendEscalating(text string, esc claudia.Escalation) (claudia.DeliveryOutcome, error)
}

// escalateOwnerToOverseer steers text into the overseer's running owner
// turn. handled=false means the overseer is not in one (or no ladder is
// configured) and the ordinary path runs.
func (s *Server) escalateOwnerToOverseer(text string) (AgentSendOutcome, bool, error) {
	s.mu.RLock()
	ladderFn := s.ownerEscalation
	ownerTurn := s.waiting && s.overseerOwnerTurn
	seat := s.overseerEscalatorSeam
	s.mu.RUnlock()
	if ladderFn == nil || !ownerTurn {
		return AgentSendOutcome{}, false, nil
	}
	ladder, ok := ladderFn()
	if !ok || len(ladder) == 0 {
		return AgentSendOutcome{}, false, nil
	}
	var proc overseerEscalator
	if seat != nil {
		proc = seat
	} else if p := s.CurrentProcess(); p != nil {
		proc = p
	}
	// Only a turn the seat itself reports running is steered; the chat
	// layer's flag alone could send a fresh prompt past the notify queue.
	if proc == nil || !proc.Alive() || proc.TurnPhase() != claudia.TurnInTurn {
		return AgentSendOutcome{}, false, nil
	}
	// 🎯T931: only rungs the overseer's seat can run. One that cannot steer
	// takes the ordinary queue-behind-turn path.
	if caps, ok := escalate.CapsOf(proc); ok {
		if ladder = escalate.Fit(ladder, caps); len(ladder) == 0 {
			slog.Info("🎯T931 overseer cannot start the owner ladder; owner message queued behind its turn",
				"steer_policy", string(caps.SteerPolicy))
			return AgentSendOutcome{}, false, nil
		}
	}

	msgID := newOwnerMessageID()
	echo := chatUserEchoID(text, msgID)
	s.NoteOwnerSend(text, echo)
	s.BroadcastChat(echo)
	out, err := proc.SendEscalating(userTurnPrefix+text, ladder)
	if err != nil {
		if escalate.NotStarted(err) {
			// Nothing reached the seat. The bubble is painted; queue behind
			// the turn as before.
			slog.Info("🎯T903 overseer cannot steer; owner message queued behind its turn", "err", err)
			if qerr := s.SendToOverseerAs(userTurnPrefix+text, msgID); qerr != nil {
				return AgentSendOutcome{}, true, qerr
			}
			return AgentSendOutcome{Status: "queued", Mechanism: delivery.MechanismQueueUntilIdle}, true, nil
		}
		slog.Error("🎯T903 escalating owner send to overseer failed", "err", err)
		s.NoteOwnerResidual("delivery_failed")
		return AgentSendOutcome{}, true, fmt.Errorf("message not delivered: %w", err)
	}
	s.NoteOwnerDelivered()
	s.observeProviderOK()
	status, verb := "steered", "steered into its running turn"
	if ladder[0].Mode == claudia.DeliverySubmit {
		status, verb = "sent", "queued with the overseer behind its running turn"
	}
	res := AgentSendOutcome{Status: status, Mechanism: out.Mechanism}
	msg := fmt.Sprintf("the overseer is busy: message %s (owner urgency", verb)
	if len(ladder) > 1 {
		res.InterruptAfterMS = ladder[1].After.Milliseconds()
		msg += fmt.Sprintf("; the turn is interrupted after %s if it has not taken the message", ladder[1].After)
	}
	res.Message = msg + ", 🎯T903)."
	slog.Info("agent_send", "component", "agent_send", "name", s.overseerAgentName(),
		"status", status, "mechanism", out.Mechanism, "outcome", "escalating", "class", "owner")
	return res, true, nil
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/escalate"
)

// ownerReceiptQueryTimeout matches the fleet held-receipt read. One
// unavailable receipt service must not wait longer than that.
const ownerReceiptQueryTimeout = 10 * time.Second

// errSendReceiptQueryUnconfigured means the daemon has not installed a
// receipt read. Callers fail closed and do not dial Claudia.
var errSendReceiptQueryUnconfigured = errors.New("send receipt query is not configured")

// 🎯T903: an owner message to an overseer that is mid-way through an owner
// turn used to wait in the notify queue until that turn ended — five minutes
// on 2026-09-29, until the owner pressed Steer by hand. Now it takes the
// owner escalation ladder, as a message to any agent does (🎯T899): steered
// into the running turn at once, and the turn interrupted after the bound if
// the overseer has not taken it. A fleet-note turn is still interrupted
// outright for the owner (🎯T291); that path is unchanged.

// SetOwnerEscalation installs the owner urgency ladder (config, via the fleet
// layer). A nil func or a false return leaves the queue-behind-turn path.
func (s *Server) SetOwnerEscalation(fn func() (escalate.Ladder, bool)) {
	s.mu.Lock()
	s.ownerEscalation = fn
	s.mu.Unlock()
}

// overseerEscalator is the part of the overseer's seat the ladder needs.
type overseerEscalator interface {
	SendEscalating(text string, esc escalate.Ladder) (claudia.DeliveryOutcome, error)
}

// requestIDEscalator is the production seat. SendEscalating passes an
// empty id into Claudia, so a seat that can carry one must be given the
// id minted at acceptance. A test double without the method stays on
// SendEscalating.
type requestIDEscalator interface {
	SendEscalatingWithRequestID(text string, esc escalate.Ladder, requestID string) (claudia.DeliveryOutcome, error)
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
	if proc == nil || s.seatState(s.overseerSeatName()).Alive != seatstate.Yes || s.seatState(s.overseerSeatName()).InFlight != seatstate.Yes {
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
	wire := userTurnPrefix + text
	var out claudia.DeliveryOutcome
	var err error
	boundID := ""
	if typed, ok := proc.(requestIDEscalator); ok {
		if err = s.freezeOverseerMission(msgID); err != nil {
			slog.Error("🎯T903 escalating owner send to overseer failed", "err", err)
			s.NoteOwnerResidual("delivery_failed")
			return AgentSendOutcome{}, true, fmt.Errorf("message not delivered: %w", err)
		}
		boundID = msgID
		out, err = typed.SendEscalatingWithRequestID(wire, ladder, msgID)
	} else {
		out, err = proc.SendEscalating(wire, ladder)
	}
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
	// A nil ladder return is the socket write. Owner delivery is the
	// receipt for this minted request. An untyped seat has no request
	// identity to read, and a nil query does not dial.
	if boundID != "" {
		s.noteOwnerDeliveredFromReceipt(boundID)
	}
	// 🎯T906: steering into a running turn is delivery, not proof the provider
	// answered — that only comes from authored assistant text or a real reply.
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

// SetSendReceiptQuery installs the production receipt read for owner
// escalation. Nil does not dial Claudia.
func (s *Server) SetSendReceiptQuery(fn func(context.Context, claudia.ReceiptQuery) (*claudia.RequestReceipt, error)) {
	s.mu.Lock()
	s.querySendReceipt = fn
	s.mu.Unlock()
}

// LookupSendReceipt reads one receipt through the installed query.
// A nil query returns errSendReceiptQueryUnconfigured and does not dial.
func (s *Server) LookupSendReceipt(ctx context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
	if s == nil {
		return nil, errSendReceiptQueryUnconfigured
	}
	s.mu.RLock()
	query := s.querySendReceipt
	s.mu.RUnlock()
	if query == nil {
		return nil, errSendReceiptQueryUnconfigured
	}
	return query(ctx, q)
}

// noteOwnerDeliveredFromReceipt sets the owner-delivery flag only when a
// settling receipt names this seat, its registry session, and requestID.
// Unknown, a missing session, a query error, or a nil query leaves the
// flag clear. Reason is not a cause and is not consulted.
func (s *Server) noteOwnerDeliveredFromReceipt(requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	s.mu.RLock()
	name := s.overseerName
	reg := s.registry
	s.mu.RUnlock()
	sessionID := ""
	if reg != nil && name != "" {
		if d := reg.Def(name); d != nil {
			sessionID = strings.TrimSpace(d.SessionID)
		}
	}
	if sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ownerReceiptQueryTimeout)
	defer cancel()
	r, err := s.LookupSendReceipt(ctx, claudia.ReceiptQuery{Seat: name, SessionID: sessionID, RequestID: requestID})
	if err != nil || r == nil {
		if err != nil && !errors.Is(err, errSendReceiptQueryUnconfigured) {
			slog.Debug("owner escalation receipt unavailable; delivery not confirmed", "seat", name, "request_id", requestID, "err", err)
		}
		return
	}
	if err := r.ValidateIdentity(name, sessionID, requestID); err != nil {
		slog.Debug("owner escalation receipt rejected", "seat", name, "request_id", requestID, "err", err)
		return
	}
	if !ownerReceiptStateSettles(r.State) {
		return
	}
	s.NoteOwnerDelivered()
}

// ownerReceiptStateSettles is the transport-acceptance fact. Failed and
// cancelled execution still settle the obligation. Unknown does not.
func ownerReceiptStateSettles(state claudia.ReceiptState) bool {
	switch state {
	case claudia.ReceiptAccepted, claudia.ReceiptStarted, claudia.ReceiptCompleted, claudia.ReceiptCancelled, claudia.ReceiptFailed:
		return true
	default:
		return false
	}
}

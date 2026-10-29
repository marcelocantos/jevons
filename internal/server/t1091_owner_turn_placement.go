// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"log/slog"
	"time"

	"github.com/marcelocantos/claudia"
)

// defaultOwnerQueuedBehindGrace is how long an owner turn the provider queued
// behind another turn may wait for that turn before it preempts it (🎯T1091).
// A prompt the provider queues against a seat it then finds idle starts within
// milliseconds; a turn already running is what the grace waits out.
const defaultOwnerQueuedBehindGrace = 2 * time.Second

// clearOwnerTurnPlacementLocked forgets where the provider put the owner turn
// in flight. Every site that ends the server's owner turn calls it. Caller
// holds s.mu.
func (s *Server) clearOwnerTurnPlacementLocked() {
	s.overseerOwnerTurnRequestID = ""
	s.overseerOwnerTurnMsgID = ""
	s.overseerOwnerQueuedBehind = false
}

// observeOwnerTurnPlacement reads, from the provider's own events, where the
// owner turn this server holds actually is (🎯T1091). It reports whether ev is
// the end of a different turn that the owner turn is queued behind; that end
// is not the owner turn's.
//
// The server's `waiting` flag knows only the turns it opened. A turn the
// provider began for a predecessor — the previous daemon, or the process a
// plan-policy park replaced — keeps running on the sidecar, and the notify
// queue drains the owner's turn into it as if the seat were idle. The sidecar
// says so: it accepts the request without a turn of its own (queued behind
// the running one). That is when the owner turn preempts (🎯T291), and the
// running turn's terminal must leave the owner turn open until the owner's own
// turn begins.
func (s *Server) observeOwnerTurnPlacement(ev claudia.Event) (foreignEnd bool) {
	s.mu.Lock()
	if _, ok := s.ownerUnanswered[ev.RequestID]; ok && ev.RequestID != "" && ownerTurnRan(ev) {
		// A turn reported unanswered answered after all: the seat kept it.
		delete(s.ownerUnanswered, ev.RequestID)
		s.mu.Unlock()
		slog.Info("notify_queue", "component", "notify_queue", "decision", "unanswered_owner_turn_answered", "request_id", ev.RequestID)
		s.broadcastDeliveryFrame("delivered", ev.RequestID, "the overseer answered this message after all", "")
		return false
	}
	held := s.overseerOwnerTurnRequestID
	if s.overseerOwnerTurn && abortedEarlierTurn(ev, held) {
		s.mu.Unlock()
		return true
	}
	if !s.overseerOwnerTurn || held == "" || ev.RequestID != held && ev.RequestID != "" && !s.overseerOwnerQueuedBehind {
		// The acceptance can arrive while the drain's handoff is still
		// returning, before the drain records the request it handed over
		// (J30: accepted 10:31:55.277, drain logged 10:31:55.289). Keep it for
		// the drain to read.
		switch {
		case ev.RequestID == "":
		case ownerTurnQueuedBehind(ev):
			s.ownerQueuedBehindEarly = ev.RequestID
		case ev.RequestID == s.ownerQueuedBehindEarly && (ownerTurnRan(ev) || ownerTurnClosedUnrun(ev)):
			s.ownerQueuedBehindEarly = ""
		}
		s.mu.Unlock()
		return false
	}
	if ev.RequestID == held && ownerTurnQueuedBehind(ev) {
		preempt := s.markOwnerQueuedBehindLocked()
		s.mu.Unlock()
		preempt()
		return false
	}
	if !s.overseerOwnerQueuedBehind {
		s.mu.Unlock()
		return false
	}
	switch {
	case ev.RequestID == held && ownerTurnRan(ev):
		// The owner turn began, or a running turn took it in: from here its
		// own end is the owner turn's end.
		s.overseerOwnerQueuedBehind = false
	case ev.RequestID == held && ownerTurnClosedUnrun(ev):
		// The provider closed it without running it. The turn it waited
		// behind ends the server's turn as before; the owner hears it was
		// not answered.
		s.overseerOwnerQueuedBehind = false
		msgID, text := s.overseerOwnerTurnMsgID, s.overseerOwnerTurnText
		report := s.recordOwnerUnansweredLocked(msgID, text)
		s.mu.Unlock()
		if report {
			s.announceOwnerUnanswered(msgID, "the overseer dropped it unanswered; it was not resent")
		}
		return false
	case ev.IsTerminalStop() || ev.Type == "system":
		foreignEnd = true
	}
	s.mu.Unlock()
	return foreignEnd
}

// recordOwnerUnansweredLocked keeps an owner turn that will not be answered
// for the owner's Resend. It reports whether there is a cockpit row to tell.
// Caller holds s.mu.
func (s *Server) recordOwnerUnansweredLocked(msgID, text string) bool {
	if msgID == "" || text == "" {
		return false
	}
	if s.ownerUnanswered == nil {
		s.ownerUnanswered = map[string]string{}
	}
	s.ownerUnanswered[msgID] = text
	return true
}

// announceOwnerUnanswered tells the owner, on that message's own row, that it
// was not answered and can be resent (the 🎯T811 Resend control).
func (s *Server) announceOwnerUnanswered(msgID, reason string) {
	slog.Warn("notify_queue",
		"component", "notify_queue",
		"decision", "owner_turn_unanswered",
		"msg_id", msgID,
		"reason", reason,
	)
	s.broadcastDeliveryFrame("undelivered", msgID, "message not answered — "+reason, reason)
}

// markOwnerQueuedBehindLocked records that the held owner turn waits behind a
// turn this server does not hold, and returns the call that arms its preempt.
// Caller holds s.mu and runs the returned func after releasing it.
func (s *Server) markOwnerQueuedBehindLocked() func() {
	if s.overseerOwnerQueuedBehind {
		return func() {}
	}
	s.overseerOwnerQueuedBehind = true
	s.overseerOwnerQueuedGen++
	gen := s.overseerOwnerQueuedGen
	held := s.overseerOwnerTurnRequestID
	grace := s.ownerQueuedBehindGrace
	if grace <= 0 {
		grace = defaultOwnerQueuedBehindGrace
	}
	return func() {
		slog.Info("notify_queue",
			"component", "notify_queue",
			"decision", "owner_queued_behind_unheld_turn",
			"request_id", held,
			"grace", grace,
		)
		// Off the event pump: the interrupt writes to the seat this event
		// came from.
		time.AfterFunc(grace, func() { s.preemptForQueuedOwnerTurn(gen) })
	}
}

// noteOwnerHandoffLocked records the owner batch the drain just handed over,
// and picks up an acceptance that arrived before this did. Caller holds s.mu
// and runs the returned func after releasing it.
func (s *Server) noteOwnerHandoffLocked(requestID, msgID string) func() {
	s.overseerOwnerTurnRequestID = requestID
	s.overseerOwnerTurnMsgID = msgID
	if requestID == "" || s.ownerQueuedBehindEarly != requestID {
		return func() {}
	}
	s.ownerQueuedBehindEarly = ""
	return s.markOwnerQueuedBehindLocked()
}

// abortedEarlierTurn is the abort terminal of a request other than the held
// owner turn's (🎯T1295). The server settles a cancel at once and the owner
// may send before the provider's own abort arrives: J3 on codex handed the
// replacement over first, and the late abort ended it, so its reply streamed
// as a non-owner turn and was suppressed.
func abortedEarlierTurn(ev claudia.Event, held string) bool {
	return held != "" && ev.IsTerminalStop() && ev.Reason == claudia.ReasonAborted &&
		ev.RequestID != "" && ev.RequestID != held
}

// ownerTurnQueuedBehind is the sidecar accepting a request without opening a
// turn for it: it is queued behind the turn that is running.
func ownerTurnQueuedBehind(ev claudia.Event) bool {
	return ev.Type == "progress" && ev.ProgressType == claudia.ProgressPromptAccepted &&
		ev.TurnID == "" && (ev.Receipt == nil || ev.Receipt.State == claudia.ReceiptAccepted)
}

// ownerTurnRan is evidence that a provider turn is carrying the request: its
// own turn began, or a running turn absorbed it.
func ownerTurnRan(ev claudia.Event) bool {
	if ev.Type == "receipt" {
		return ev.Receipt != nil && ev.Receipt.State == claudia.ReceiptStarted
	}
	return ev.TurnID != "" || ev.ProgressType == claudia.ProgressDeliveryAbsorbed
}

// ownerTurnClosedUnrun is a receipt that closes the request without a turn.
func ownerTurnClosedUnrun(ev claudia.Event) bool {
	if ev.Type != "receipt" || ev.Receipt == nil {
		return false
	}
	return ev.Receipt.State == claudia.ReceiptCancelled || ev.Receipt.State == claudia.ReceiptFailed
}

// preemptForQueuedOwnerTurn interrupts the turn an owner turn is still queued
// behind once the grace has passed (🎯T291 for a turn this server did not open).
func (s *Server) preemptForQueuedOwnerTurn(gen uint64) {
	s.mu.Lock()
	still := s.overseerOwnerTurn && s.overseerOwnerQueuedBehind && s.overseerOwnerQueuedGen == gen
	held := s.overseerOwnerTurnRequestID
	s.mu.Unlock()
	if !still {
		return
	}
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "owner_preempt_unheld_turn",
		"request_id", held,
	)
	s.interruptOverseerForOwner()
}

// settleForeignTurnEnd closes the stream of a turn that ended while the owner
// turn waits behind it. The owner turn stays held: nothing drains over it.
func (s *Server) settleForeignTurnEnd() {
	s.mu.Lock()
	s.turnBuf = ""
	s.overseerStreamID = ""
	s.overseerStreamAcc = ""
	s.overseerStreamSilent = false
	s.overseerStreamHold = nil
	held := s.overseerOwnerTurnRequestID
	s.noteOverseerProgressLocked()
	s.mu.Unlock()
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "unheld_turn_ended_owner_turn_waits",
		"request_id", held,
	)
	s.setOverseerPhase(PhaseSample{Phase: PhaseAccepted})
}

// abandonReplacedOverseerTurn ends the turn the server holds for a process
// that was replaced before it ended (🎯T1091). A plan-policy park stops the
// seat mid-turn and the recovery sweep re-mints it; that turn's terminal never
// arrives. An owner turn among them is not lost silently: the owner is told it
// was not answered and offered Resend. It is not resent here — its bytes
// already reached the old seat, and a second unasked delivery is the duplicate
// 🎯T416 forbids. If the seat answers after all, the report flips to delivered.
func (s *Server) abandonReplacedOverseerTurn() {
	s.abandonOverseerTurn("the overseer was stopped before it answered; it was not resent")
}

// abandonStuckOverseerTurn ends the turn the stuck-busy recovery just
// interrupted (🎯T1094). Same contract as abandonReplacedOverseerTurn: an
// owner turn is reported with Resend, not resent, and a late answer flips it.
func (s *Server) abandonStuckOverseerTurn() {
	s.abandonOverseerTurn("the overseer was stuck and was interrupted before it answered; it was not resent")
}

// abandonOverseerTurn settles the turn the server holds and, when it is the
// owner's, tells the owner why it went unanswered.
func (s *Server) abandonOverseerTurn(reason string) {
	s.mu.Lock()
	msgID, text := s.overseerOwnerTurnMsgID, s.overseerOwnerTurnText
	report := s.overseerOwnerTurn && s.recordOwnerUnansweredLocked(msgID, text)
	s.mu.Unlock()
	s.settleOverseerAfterUnstick()
	if report {
		s.announceOwnerUnanswered(msgID, reason)
	}
}

// takeUnansweredOwnerTurn removes and returns an owner turn reported
// unanswered, for the owner's Resend.
func (s *Server) takeUnansweredOwnerTurn(msgID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, ok := s.ownerUnanswered[msgID]
	if ok {
		delete(s.ownerUnanswered, msgID)
	}
	return text, ok
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/escalate"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T1130: one visible queue for a message the owner sent, whichever side
// is holding it. A message the daemon holds behind a running turn
// (s.notifyQueue, announced via noteOwnerHeld) is pulled back out here and
// delivered under the owner's chosen mode, instead of waiting out the FIFO.
// This never leaves a second copy behind: the text is physically removed
// from notifyQueue before the escalating send runs, and put back if that
// send cannot be started.

// takeQueuedOwnerMessage removes the earliest queued occurrence of the text
// registered under id from s.notifyQueue, and returns the owner's own words
// (the userTurnPrefix wire wrapper stripped). ok is false when id is not
// currently waiting in the queue — already drained, already delivered, or
// never queued.
func (s *Server) takeQueuedOwnerMessage(id string) (text string, ok bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	wire := ""
	for t, ids := range s.notifyOwnerIDs {
		if slices.Contains(ids, id) {
			wire = t
			break
		}
	}
	if wire == "" {
		return "", false
	}
	// Identical owner texts retain distinct FIFO ids. The nth id of this
	// wire belongs to the nth queued occurrence, not always the first.
	ordinal := slices.Index(s.notifyOwnerIDs[wire], id)
	seen := 0
	removed := false
	out := s.notifyQueue[:0:0]
	for _, t := range s.notifyQueue {
		if t == wire {
			if !removed && seen == ordinal {
				removed = true
				continue
			}
			seen++
		}
		out = append(out, t)
	}
	if !removed {
		return "", false
	}
	s.notifyQueue = out
	s.observeOwnerQueueLocked()
	next := s.notifyOwnerIDs[wire][:0:0]
	for _, i := range s.notifyOwnerIDs[wire] {
		if i == id {
			continue
		}
		next = append(next, i)
	}
	if len(next) == 0 {
		delete(s.notifyOwnerIDs, wire)
	} else {
		s.notifyOwnerIDs[wire] = next
	}
	delete(s.ownerHeld, ownerStateKey(id, wire))
	delete(s.ownerUndelivered, ownerStateKey(id, wire))
	return strings.TrimPrefix(wire, userTurnPrefix), true
}

// requeueOwnerMessage puts text back at the FRONT of the queue under id,
// unchanged from how takeQueuedOwnerMessage found it, and re-announces held
// so the cockpit does not silently drop the Steer/Cut-in chrome. Used only
// when EscalateOwnerHeld could not start the seat.
func (s *Server) requeueOwnerMessage(text, id string) {
	wire := userTurnPrefix + text
	s.mu.Lock()
	s.notifyQueue = append([]string{wire}, s.notifyQueue...)
	s.observeOwnerQueueLocked()
	// Requeue puts the wire at the front; its identity must follow that
	// position even when an identical text is already queued.
	s.notifyOwnerIDs[wire] = append([]string{id}, s.notifyOwnerIDs[wire]...)
	s.mu.Unlock()
	s.persistOwnerQueue()
	s.noteOwnerHeld(wire, id)
}

// EscalateOwnerHeld pulls a daemon-held owner message (announced "held" by
// noteOwnerHeld) out of the queue and delivers it now under mode: steer
// folds it into the overseer's running turn, interrupt cancels that turn
// first. submit/queue are refused — they would not change what the owner
// already sees (the message is already queued in FIFO order).
func (s *Server) EscalateOwnerHeld(id string, mode delivery.Mode) error {
	if mode != delivery.ModeSteer && mode != delivery.ModeInterrupt {
		return fmt.Errorf("escalate mode must be steer or interrupt, got %q", mode)
	}
	text, ok := s.takeQueuedOwnerMessage(id)
	if !ok {
		return fmt.Errorf("message %s is not waiting in the queue", id)
	}
	s.persistOwnerQueue()

	ladder := escalate.Ladder{{Mode: claudia.DeliverySteer}}
	if mode == delivery.ModeInterrupt {
		// Claudia only accepts submit/steer as the first rung. Cut in
		// starts with steer, then interrupts almost immediately if the
		// running turn has not already absorbed the message.
		ladder = append(ladder, claudia.EscalationStep{Mode: claudia.DeliveryInterrupt, After: time.Millisecond})
	}

	s.mu.RLock()
	seat := s.overseerEscalatorSeam
	s.mu.RUnlock()
	var proc overseerEscalator
	if seat != nil {
		proc = seat
	} else if p := s.CurrentProcess(); p != nil {
		proc = p
	}
	if proc == nil || s.seatState(s.overseerSeatName()).Alive != seatstate.Yes {
		s.requeueOwnerMessage(text, id)
		return fmt.Errorf("overseer %q is not reachable; message re-queued", s.overseerAgentName())
	}

	wire := userTurnPrefix + text
	var err error
	if typed, ok := proc.(requestIDEscalator); ok {
		if ferr := s.freezeOverseerMission(id); ferr != nil {
			s.requeueOwnerMessage(text, id)
			return fmt.Errorf("escalate %s failed: %w", mode, ferr)
		}
		_, err = typed.SendEscalatingWithRequestID(wire, ladder, id)
	} else {
		_, err = proc.SendEscalating(wire, ladder)
	}
	if err != nil {
		s.requeueOwnerMessage(text, id)
		slog.Info("🎯T1130 escalate of a held owner message failed; re-queued", "mode", string(mode), "err", err)
		return fmt.Errorf("escalate %s failed: %w", mode, err)
	}
	// The text is no longer in notifyQueue. Retire its held chrome as soon
	// as the seat accepted the escalating send; a subsequent receipt is
	// needed to prove model delivery, but not to keep showing queue actions
	// for a message that cannot be escalated a second time.
	s.noteOwnerDeliveredID(id)
	s.noteOwnerDeliveredFromReceipt(id)
	s.persistOwnerQueue()
	s.broadcastDeliveryFrame("delivered", id, "message handed to the overseer", "")
	return nil
}

// RemoveOwnerHeld withdraws only an owner message still in the daemon queue.
// A delivered or unknown id cannot be erased from a transcript by this path.
func (s *Server) RemoveOwnerHeld(id string) error {
	_, ok := s.takeQueuedOwnerMessage(id)
	if !ok {
		return fmt.Errorf("message %s is not waiting in the queue", id)
	}
	s.persistOwnerQueue()
	s.broadcastDeliveryFrame("removed", id, "message removed from queue", "")
	return nil
}

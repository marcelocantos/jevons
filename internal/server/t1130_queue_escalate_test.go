// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T1130: a message that lands behind a running turn — whatever the
// browser's own phase belief was — announces itself as "held", the same
// frame family as T811's undelivered/delivered, so the cockpit can paint
// Steer / Cut-in instead of a plain bubble. Steer/Cut-in then pulls the
// message out of the queue and delivers it under that mode, never leaving a
// second copy behind.
func TestT1130HeldAnnouncedAndEscalated(t *testing.T) {
	s := &Server{}
	s.notifyRetryDelay = time.Hour
	lines := make(chan string, 16)
	s.chatListeners = append(s.chatListeners, lines)
	s.notifySender = func(string) error { return nil }

	drain := func() []map[string]any {
		var out []map[string]any
		for {
			select {
			case l := <-lines:
				var m map[string]any
				if json.Unmarshal([]byte(l), &m) == nil {
					out = append(out, m)
				}
			default:
				return out
			}
		}
	}

	// The daemon's own truth is busy, whatever the browser believed when it
	// sent (stale-idle case (b)): a plain submit lands behind the turn.
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()

	if err := s.SendToOverseerAs(userTurnPrefix+"what's the status", "om-1"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	held := drain()
	var sawHeld bool
	for _, f := range held {
		if f["type"] == "send_error" && f["state"] == "held" && f["msg_id"] == "om-1" {
			sawHeld = true
		}
	}
	if !sawHeld {
		t.Fatalf("no held frame for the stale-idle send; got %v", held)
	}

	seat := &fakeOverseerSeat{phase: claudia.TurnInTurn}
	s.SetOverseerName("jevons")
	seats := seatstate.New(seatstate.Args{})
	s.SetSeats(seats)
	seats.Observe(seatstate.Observation{Name: "jevons", Alive: seatstate.Yes, InFlight: seatstate.Yes, QueueDepth: seatstate.QueueUnknown, Source: "test.seat"})
	s.mu.Lock()
	s.overseerEscalatorSeam = seat
	s.mu.Unlock()

	if err := s.EscalateOwnerHeld("om-1", delivery.ModeSteer); err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if len(seat.sent) != 1 || seat.sent[0] != userTurnPrefix+"what's the status" {
		t.Fatalf("seat got %v, want exactly one steered copy", seat.sent)
	}
	var sawDelivered bool
	for _, f := range drain() {
		if f["type"] == "send_error" && f["state"] == "delivered" && f["msg_id"] == "om-1" {
			sawDelivered = true
		}
	}
	if !sawDelivered {
		t.Fatal("escalated message left in held chrome after the seat accepted it")
	}
	s.mu.Lock()
	remaining := len(s.notifyQueue)
	s.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("message left behind in the queue after escalate: %d", remaining)
	}

	// Escalating an id that is not (or no longer) queued is a loud error,
	// never a silent no-op or a second send.
	if err := s.EscalateOwnerHeld("om-1", delivery.ModeSteer); err == nil {
		t.Fatal("re-escalating an already-delivered id must error")
	}
	if len(seat.sent) != 1 {
		t.Fatalf("re-escalate sent again: %v", seat.sent)
	}
}

// Cut in must use Claudia's valid steer-then-interrupt ladder rather than
// an invalid interrupt-first rung; the same row must leave the queue.
func TestT1130CutInUsesValidLadderAndRetiresHeldRow(t *testing.T) {
	seat := &fakeOverseerSeat{phase: claudia.TurnInTurn}
	s := t903Server(seat, true)
	s.notifyRetryDelay = time.Hour
	lines := make(chan string, 16)
	s.chatListeners = append(s.chatListeners, lines)
	if err := s.SendToOverseerAs(userTurnPrefix+"cut in", "om-cut"); err != nil {
		t.Fatal(err)
	}
	if err := s.EscalateOwnerHeld("om-cut", delivery.ModeInterrupt); err != nil {
		t.Fatal(err)
	}
	if len(seat.ladders) != 1 || len(seat.ladders[0]) != 2 ||
		seat.ladders[0][0].Mode != claudia.DeliverySteer ||
		seat.ladders[0][1].Mode != claudia.DeliveryInterrupt ||
		seat.ladders[0][1].After <= 0 {
		t.Fatalf("Cut in ladder = %+v", seat.ladders)
	}
	if err := claudia.Escalation(seat.ladders[0]).Validate(); err != nil {
		t.Fatalf("Cut in ladder refused by Claudia: %v", err)
	}
	var delivered bool
	for len(lines) > 0 {
		var f map[string]any
		if json.Unmarshal([]byte(<-lines), &f) == nil && f["state"] == "delivered" && f["msg_id"] == "om-cut" {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("Cut in did not retire the held row")
	}
}

// Escalate refuses submit/queue — only steer and interrupt change what the
// owner already sees queued.
func TestT1130EscalateRefusesNonEscalatingModes(t *testing.T) {
	s := &Server{}
	s.notifyRetryDelay = time.Hour
	s.notifySender = func(string) error { return nil }
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	if err := s.SendToOverseerAs(userTurnPrefix+"hi", "om-2"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []delivery.Mode{delivery.ModeSubmit, delivery.ModeQueue} {
		if err := s.EscalateOwnerHeld("om-2", m); err == nil {
			t.Fatalf("mode %q accepted; only steer/interrupt may escalate a held entry", m)
		}
	}
	s.mu.Lock()
	remaining := len(s.notifyQueue)
	s.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("a refused escalate must not disturb the queue: %d", remaining)
	}
}

// An unreachable seat re-queues rather than losing the message.
func TestT1130EscalateRequeuesOnUnreachableSeat(t *testing.T) {
	s := &Server{}
	s.notifyRetryDelay = time.Hour
	s.notifySender = func(string) error { return nil }
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	if err := s.SendToOverseerAs(userTurnPrefix+"ping", "om-3"); err != nil {
		t.Fatal(err)
	}
	if err := s.EscalateOwnerHeld("om-3", delivery.ModeInterrupt); err == nil {
		t.Fatal("no overseer process reachable; want an error")
	}
	s.mu.Lock()
	remaining := len(s.notifyQueue)
	s.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("message dropped instead of re-queued: %d", remaining)
	}
	if err := s.EscalateOwnerHeld("om-3", delivery.ModeInterrupt); err == nil {
		fmt.Println("escalate eventually succeeded without a seat; unexpected")
	}
}

func TestT1130RemoveHeldOnlyWithdrawsSelectedDuplicate(t *testing.T) {
	s := &Server{}
	s.notifyRetryDelay = time.Hour
	s.notifySender = func(string) error { return nil }
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	wire := userTurnPrefix + "same words"
	for _, id := range []string{"om-first", "om-second"} {
		if err := s.SendToOverseerAs(wire, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RemoveOwnerHeld("om-second"); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	if len(s.notifyQueue) != 1 || s.notifyQueue[0] != wire || len(s.notifyOwnerIDs[wire]) != 1 || s.notifyOwnerIDs[wire][0] != "om-first" {
		t.Errorf("wrong remaining queue: %v / %v", s.notifyQueue, s.notifyOwnerIDs[wire])
	}
	s.mu.RUnlock()
	if err := s.RemoveOwnerHeld("om-second"); err == nil {
		t.Fatal("removed an id twice")
	}
}

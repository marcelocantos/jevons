// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1094: the stuck-busy recovery interrupts the overseer and settles the
// turn the server holds. When that turn is the owner's, settling it silently
// leaves the owner's message looking sent and never answered. It must be
// reported on its own row with the 🎯T811 Resend, never resent unasked
// (🎯T416), and a late answer must withdraw the report — as 🎯T1091 does for
// an overseer replaced mid-turn.
func TestT1094StuckBusyUnstickReportsTheOwnerTurnWithResend(t *testing.T) {
	s, _, sent := replacedOverseerServer(t)
	lines := make(chan string, 32)
	s.chatListeners = append(s.chatListeners, lines)
	s.mu.Lock()
	s.proc = claudia.NewStubAgent(func(string) error { return nil }) // the stuck seat
	s.mu.Unlock()

	const owner = userTurnPrefix + "is the fleet still up?"
	if err := s.SendToOverseerAs(owner, "om-stuck"); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 {
		t.Fatalf("owner handoff = %v", *sent)
	}
	sendErrorFrames(lines)

	// The seat stops making progress; the cockpit's stuck-busy recovery runs
	// its first (interrupt, not relaunch) attempt.
	if err := s.cockpitUnstickBusy(&cockpitState{}, cockpitObs{Waiting: true, PromptInFlight: true}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	waiting, ownerTurn := s.waiting, s.overseerOwnerTurn
	s.mu.Unlock()
	if waiting || ownerTurn {
		t.Fatalf("after unstick: waiting=%v owner_turn=%v, want the turn settled", waiting, ownerTurn)
	}
	if len(*sent) != 1 {
		t.Fatalf("handoffs after unstick = %v, want the owner turn not resent unasked (🎯T416)", *sent)
	}
	var report map[string]any
	for _, f := range sendErrorFrames(lines) {
		if f["msg_id"] == "om-stuck" && f["state"] == "undelivered" {
			report = f
		}
	}
	if report == nil {
		t.Fatal("the stuck-busy unstick settled the owner turn without telling the owner")
	}
	if r, _ := report["reason"].(string); r == "" {
		t.Fatalf("report has no reason: %v", report)
	}

	// The interrupted seat answers after all: the report flips to delivered
	// and Resend no longer offers a duplicate.
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "yes, all up", TurnID: "t-stuck", RequestID: "om-stuck"})
	var flipped bool
	for _, f := range sendErrorFrames(lines) {
		if f["msg_id"] == "om-stuck" && f["state"] == "delivered" {
			flipped = true
		}
	}
	if !flipped {
		t.Fatal("a late answer did not flip the unanswered report")
	}
	if err := s.ResendOwnerMessage("om-stuck"); err == nil {
		t.Fatal("Resend accepted for a message the overseer answered")
	}
	if len(*sent) != 1 {
		t.Fatalf("handoffs = %v", *sent)
	}
}

// Without a late answer, the owner's Resend sends it once more, once.
func TestT1094StuckBusyUnstickOwnerTurnResendsOnce(t *testing.T) {
	s, _, sent := replacedOverseerServer(t)
	s.mu.Lock()
	s.proc = claudia.NewStubAgent(func(string) error { return nil })
	s.mu.Unlock()
	const owner = userTurnPrefix + "status?"
	if err := s.SendToOverseerAs(owner, "om-stuck2"); err != nil {
		t.Fatal(err)
	}
	if err := s.cockpitUnstickBusy(&cockpitState{}, cockpitObs{Waiting: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResendOwnerMessage("om-stuck2"); err != nil {
		t.Fatalf("resend: %v", err)
	}
	if len(*sent) != 2 || (*sent)[1] != owner {
		t.Fatalf("handoffs after Resend = %v, want the owner turn once more", *sent)
	}
	if err := s.ResendOwnerMessage("om-stuck2"); err == nil {
		t.Fatal("a second Resend was accepted")
	}
}

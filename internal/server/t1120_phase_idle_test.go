// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// TestT1120OwnerAnswerReturnsPhaseIdle is the isolate-facing half of
// 🎯T1120 clause 3, hermetically: one owner send, a system re-offer of
// the same words while the answer is in flight (the 2026-10-21 20:56
// specimen), then a sealed reply. The mux level the cockpit reduces
// must be idle, not stuck on received with a fleet correspondent.
func TestT1120OwnerAnswerReturnsPhaseIdle(t *testing.T) {
	s, sess := t919Server(t)
	var delivered []string
	s.notifySender = func(text string) error {
		delivered = append(delivered, text)
		return nil
	}

	text := "In one short paragraph: what is the fleet doing right now?"
	wire := userTurnPrefix + text
	if err := s.SendToOverseerAs(wire, "om-1"); err != nil {
		t.Fatalf("first owner send: %v", err)
	}
	if _, err := s.sendToOverseerAs(wire, ""); err != nil {
		t.Fatalf("re-offer while in flight: %v", err)
	}
	if len(delivered) != 1 {
		t.Fatalf("after in-flight re-offer delivered=%d, want exactly one provider request", len(delivered))
	}

	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "Fleet status: Healthy."})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	assertOverseerIdle(t, s, sess)
	if s.overseerWorkingLevel() {
		t.Fatal("owner working chrome still lit after the answer sealed")
	}
}

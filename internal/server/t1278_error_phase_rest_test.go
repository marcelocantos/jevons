// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T1278: a turn that ends in error answers nobody. On 2026-10-29 the
// development overseer's phase was error with correspondent [fleet] hours
// after its last turn, and the cockpit painted "error · answering fleet"
// over an idle seat.
func TestT1278ErrorPhaseClearsCorrespondent(t *testing.T) {
	s := &Server{}
	s.notifySender = func(string) error { return nil }
	cap := newPhaseCapture(s)
	_ = s.SendToOverseer(envelope.PrefixAgentResponded + "\n" + envelope.SeatFact(envelope.IntentRoute, "jevons-po") + "\nreport")
	_ = cap.frames(t)
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", IsError: true, Text: "boom"})
	fr := cap.frames(t)
	if len(fr) != 1 || fr[0]["phase"] != PhaseError {
		t.Fatalf("want one error frame, got %v", fr)
	}
	if _, has := fr[0]["correspondent"]; has || fr[0]["working"] != false {
		t.Fatalf("error must clear correspondent and working: %v", fr[0])
	}
	if p := s.OverseerPhase(); p.Phase != PhaseError || p.Correspondent != nil {
		t.Fatalf("history_meta snapshot: %+v", p)
	}
}

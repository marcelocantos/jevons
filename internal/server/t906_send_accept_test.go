// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T906: steering an owner message into the overseer's running turn is the
// seat accepting delivery (claudia.DeliveryOutcome, err == nil), not
// evidence the provider answered it. Red against the pre-fix tree, where
// escalateOwnerToOverseer's success path called s.observeProviderOK() on
// that same delivery confirmation (internal/server/t903_owner_escalate.go),
// so a steered probe cleared a hard-block before the seat's own next turn
// failed on the same wall.
func TestT906SteeredDeliveryDoesNotClearHardBlock(t *testing.T) {
	seat := &fakeOverseerSeat{phase: claudia.TurnInTurn}
	s := t903Server(seat, true)
	var failures, oks int
	s.SetProviderHardBlockHooks(func(agenterr.Class, string) { failures++ }, func() { oks++ })

	out, handled, err := s.escalateOwnerToOverseer("are you there?")
	if err != nil || !handled {
		t.Fatalf("escalateOwnerToOverseer: out=%+v handled=%v err=%v", out, handled, err)
	}
	if out.Status != "steered" {
		t.Fatalf("status=%q want steered", out.Status)
	}
	if oks != 0 {
		t.Fatalf("steered delivery called the provider-OK hook %d times, want 0", oks)
	}
	if failures != 0 {
		t.Fatalf("unexpected failure hook calls: %d", failures)
	}
}

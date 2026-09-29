// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/delivery"
)

// relayedSteerRefusal is the 2026-09-29 refusal, verbatim as the broker
// relayed it: a string, which errors.Is cannot match.
var relayedSteerRefusal = errors.New("broker protocol: agent_failed: steer unsupported: claude provider: provider has no steer mechanism (policy queue_until_idle)")

// capsOverseerSeat reports the turn capabilities a Claude Code seat reports.
type capsOverseerSeat struct {
	fakeOverseerSeat
	caps claudia.TurnCaps
}

func (f *capsOverseerSeat) TurnCaps() claudia.TurnCaps { return f.caps }

// 🎯T931: an owner message to a busy overseer whose seat cannot steer is
// queued behind its turn. The ladder is never offered, because its first
// rung is one the seat has told us it cannot run.
func TestT931OwnerMessageToNonSteerableOverseerQueues(t *testing.T) {
	seat := &capsOverseerSeat{
		fakeOverseerSeat: fakeOverseerSeat{phase: claudia.TurnInTurn, err: relayedSteerRefusal},
		caps:             claudia.ProviderTurnCaps(claudia.ProviderClaude),
	}
	s := t903Server(nil, true)
	s.mu.Lock()
	s.overseerEscalatorSeam = seat
	s.mu.Unlock()
	if _, handled, err := s.escalateOwnerToOverseer("wind up"); handled || err != nil {
		t.Fatalf("handled=%v err=%v; a seat that cannot steer takes the ordinary queue-behind-turn path", handled, err)
	}
	if len(seat.ladders) != 0 {
		t.Fatalf("a steer ladder was offered to a seat that cannot steer: %+v", seat.ladders)
	}
}

// And when a seat cannot say what it can do, the refusal it relays is still
// read as "nothing reached the seat": queued, not a delivery failure.
func TestT931RelayedSteerRefusalQueuesBehindOverseerTurn(t *testing.T) {
	seat := &fakeOverseerSeat{phase: claudia.TurnInTurn, err: relayedSteerRefusal}
	s := t903Server(seat, true)
	out, err := s.sendToNamedAgentMode(s.overseerAgentName(), "wind up", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatalf("plain owner send to a busy overseer failed: %v", err)
	}
	if out.Status != "queued" {
		t.Fatalf("outcome = %+v, want queued", out)
	}
	s.mu.Lock()
	q := strings.Join(s.notifyQueue, "|")
	s.mu.Unlock()
	if !strings.Contains(q, "wind up") {
		t.Fatalf("notify queue = %q; the message must wait for the turn", q)
	}
}

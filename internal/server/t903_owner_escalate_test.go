// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/escalate"
)

type fakeOverseerSeat struct {
	phase   claudia.TurnPhase
	sent    []string
	ladders []escalate.Ladder
	err     error
}

func (f *fakeOverseerSeat) Alive() bool                  { return true }
func (f *fakeOverseerSeat) TurnPhase() claudia.TurnPhase { return f.phase }
func (f *fakeOverseerSeat) SendEscalating(text string, esc escalate.Ladder) (claudia.DeliveryOutcome, error) {
	f.sent = append(f.sent, text)
	f.ladders = append(f.ladders, esc)
	if f.err != nil {
		return claudia.DeliveryOutcome{}, f.err
	}
	return claudia.DeliveryOutcome{Mechanism: "steer"}, nil
}

var ownerLadder = escalate.Ladder{
	{Mode: claudia.DeliverySteer},
	{Mode: claudia.DeliveryInterrupt, After: time.Minute},
}

func t903Server(seat *fakeOverseerSeat, ownerTurn bool) *Server {
	s := &Server{}
	s.notifySender = func(string) error { return nil } // hermetic drain
	s.SetOwnerEscalation(func() (escalate.Ladder, bool) { return ownerLadder, true })
	s.mu.Lock()
	s.overseerEscalatorSeam = seat
	s.waiting, s.overseerOwnerTurn = true, ownerTurn
	s.mu.Unlock()
	return s
}

// 🎯T903: with the overseer mid owner turn, the owner's next message is
// steered into it under the owner ladder, not left in the notify queue.
func TestT903OwnerMessageSteersIntoBusyOverseer(t *testing.T) {
	seat := &fakeOverseerSeat{phase: claudia.TurnInTurn}
	s := t903Server(seat, true)
	out, err := s.sendToNamedAgentMode(s.overseerAgentName(), "also check T540", sendOriginOwner, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if len(seat.sent) != 1 || !strings.HasSuffix(seat.sent[0], "also check T540") || !strings.HasPrefix(seat.sent[0], userTurnPrefix) {
		t.Fatalf("seat got %q, want the owner-framed message steered in", seat.sent)
	}
	if len(seat.ladders[0]) != 2 || seat.ladders[0][0].Mode != claudia.DeliverySteer {
		t.Fatalf("ladder = %+v", seat.ladders[0])
	}
	if out.Status != "steered" || out.InterruptAfterMS != time.Minute.Milliseconds() {
		t.Fatalf("outcome = %+v; the pane's countdown needs steered + interrupt_after_ms", out)
	}
	s.mu.Lock()
	queued := len(s.notifyQueue)
	s.mu.Unlock()
	if queued != 0 {
		t.Fatalf("notify queue holds %d; the steered message must not also wait there", queued)
	}
}

// A fleet-note turn keeps 🎯T291's immediate interrupt; an idle overseer, a
// seat that cannot steer, and an unconfigured ladder take the ordinary path.
func TestT903OrdinaryPathsUnchanged(t *testing.T) {
	for name, c := range map[string]struct {
		seat      *fakeOverseerSeat
		ownerTurn bool
		ladder    bool
	}{
		"fleet turn": {&fakeOverseerSeat{phase: claudia.TurnInTurn}, false, true},
		"seat idle":  {&fakeOverseerSeat{phase: claudia.TurnIdle}, true, true},
		"no ladder":  {&fakeOverseerSeat{phase: claudia.TurnInTurn}, true, false},
	} {
		s := t903Server(c.seat, c.ownerTurn)
		if !c.ladder {
			s.SetOwnerEscalation(func() (escalate.Ladder, bool) { return nil, false })
		}
		if _, handled, err := s.escalateOwnerToOverseer("hi"); handled || err != nil {
			t.Fatalf("%s: handled=%v err=%v; want the ordinary path", name, handled, err)
		}
		if len(c.seat.sent) != 0 {
			t.Fatalf("%s: seat was sent %q", name, c.seat.sent)
		}
	}

	// Steer unsupported: nothing reached the seat, so the message queues
	// behind the turn exactly as before.
	seat := &fakeOverseerSeat{phase: claudia.TurnInTurn, err: claudia.ErrSteerUnsupported}
	s := t903Server(seat, true)
	out, handled, err := s.escalateOwnerToOverseer("hold this")
	if !handled || err != nil || out.Status != "queued" {
		t.Fatalf("unsupported steer: %+v handled=%v err=%v", out, handled, err)
	}
	s.mu.Lock()
	q := strings.Join(s.notifyQueue, "|")
	s.mu.Unlock()
	if !strings.Contains(q, "hold this") {
		t.Fatalf("notify queue = %q; the message must wait for the turn", q)
	}
}

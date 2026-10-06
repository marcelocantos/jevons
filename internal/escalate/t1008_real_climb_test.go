// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package escalate

import (
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T1008.2: before this migration, escalate.Send fired only the first
// rung — it never actually climbed a ladder. *claudia.Agent.SendEscalating
// does climb for real (absorb detection, timed interrupt), and since
// Step/Ladder are now aliases of claudia's own EscalationStep/Escalation,
// a jevons seat offered to Handle (or any *claudia.Agent directly) gets
// that real climb. These tests drive it through claudia.NewStubAgentOps —
// an Agent with no provider process behind it, exported by claudia for
// exactly this: a dependent package exercising the product's real Agent
// rather than a parallel fake that can drift from it.

// climbSeat records the verbs claudia's own Agent issues against the stub.
type climbSeat struct {
	mu    sync.Mutex
	phase claudia.TurnPhase
	ran   []string
}

func (s *climbSeat) record(v string) {
	s.mu.Lock()
	s.ran = append(s.ran, v)
	s.mu.Unlock()
}

func (s *climbSeat) verbs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ran...)
}

func newClimbAgent(phase claudia.TurnPhase) (*claudia.Agent, *climbSeat) {
	seat := &climbSeat{phase: phase}
	a := claudia.NewStubAgentOps(&claudia.StubAgentOps{
		Provider: claudia.ProviderCursor,
		Send:     func(text string) error { seat.record("send:" + text); return nil },
		Steer: func(text string) (claudia.DeliveryOutcome, error) {
			seat.record("steer:" + text)
			return claudia.DeliveryOutcome{Mechanism: "stub_steer"}, nil
		},
		Interrupt: func() error { seat.record("interrupt"); return nil },
		TurnPhase: func() claudia.TurnPhase {
			seat.mu.Lock()
			defer seat.mu.Unlock()
			return seat.phase
		},
	})
	return a, seat
}

// An unabsorbed message on a busy seat is interrupted at its deadline —
// the real climb, driven through jevons's Handle adapter exactly as
// agent_send_escalate.go and t903_owner_escalate.go offer a seat to it.
func TestT1008RealClimbInterruptsUnabsorbedMessage(t *testing.T) {
	agent, seat := newClimbAgent(claudia.TurnInTurn)
	h := Handle{Agent: agent}

	ladder := Ladder{{Mode: claudia.DeliverySteer}, {Mode: claudia.DeliveryInterrupt, After: 60 * time.Millisecond}}
	escalated := make(chan string, 4)
	agent.SubscribeEvents(func(ev claudia.Event) {
		if ev.Type == "progress" && ev.ProgressType == claudia.ProgressDeliveryEscalated {
			escalated <- ev.Text
		}
	})

	start := time.Now()
	if _, err := h.SendEscalating("urgent", ladder); err != nil {
		t.Fatal(err)
	}
	// Blocks until the rung fires; go test's own -timeout is the clock, as
	// claudia's own equivalent test (escalation_test.go) does it.
	mode := <-escalated
	if mode != string(claudia.DeliveryInterrupt) {
		t.Fatalf("escalated with %q", mode)
	}
	if waited := time.Since(start); waited < 60*time.Millisecond {
		t.Fatalf("interrupted after %s, before its deadline", waited)
	}
	time.Sleep(30 * time.Millisecond)
	if got := seat.verbs(); len(got) != 2 || got[0] != "steer:urgent" || got[1] != "interrupt" {
		t.Fatalf("verbs = %v, want [steer:urgent interrupt]", got)
	}
}

// A message the seat absorbs before the deadline is never interrupted —
// the soft rung was enough.
func TestT1008RealClimbDoesNotInterruptAbsorbedMessage(t *testing.T) {
	agent, seat := newClimbAgent(claudia.TurnInTurn)
	h := Handle{Agent: agent}

	ladder := Ladder{{Mode: claudia.DeliverySteer}, {Mode: claudia.DeliveryInterrupt, After: 80 * time.Millisecond}}
	if _, err := h.SendEscalating("status?", ladder); err != nil {
		t.Fatal(err)
	}
	agent.PublishEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressDeliveryAbsorbed, Text: "some other message"})
	agent.PublishEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressDeliveryAbsorbed, Text: "status?"})
	time.Sleep(160 * time.Millisecond)
	got := seat.verbs()
	if len(got) != 1 || got[0] != "steer:status?" {
		t.Fatalf("verbs = %v, want [steer:status?] only — the absorbed rung must not escalate", got)
	}
}

// An idle seat is a plain submit, no ladder climbed at all — Handle's
// nil-Agent guard aside, this is the ordinary SendEscalating contract
// a jevons caller relies on when offering a seat that turns out idle.
func TestT1008RealClimbIdleSeatIsPlainSubmit(t *testing.T) {
	agent, seat := newClimbAgent(claudia.TurnIdle)
	h := Handle{Agent: agent}

	ladder := Ladder{{Mode: claudia.DeliverySteer}, {Mode: claudia.DeliveryInterrupt, After: time.Millisecond}}
	out, err := h.SendEscalating("hi", ladder)
	if err != nil || out.Mechanism != claudia.MechanismSubmit {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	time.Sleep(30 * time.Millisecond)
	if got := seat.verbs(); len(got) != 1 || got[0] != "send:hi" {
		t.Fatalf("verbs = %v, want [send:hi]", got)
	}
}

// Handle with a nil Agent (the shape callers guard against before
// offering a seat) refuses rather than panicking or silently dropping.
func TestT1008RealClimbNilAgentHandleRefuses(t *testing.T) {
	h := Handle{}
	if _, err := h.SendEscalating("hi", Ladder{{Mode: claudia.DeliverySteer}}); err == nil {
		t.Fatal("a nil-Agent Handle must refuse rather than send")
	}
}

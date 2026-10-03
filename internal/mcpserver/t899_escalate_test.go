// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/escalate"
)

// escalatingFake is a seat whose phase and verbs the test sets and reads.
type escalatingFake struct {
	phase   claudia.TurnPhase
	sent    []string
	ladders []escalate.Ladder
	err     error
}

func (f *escalatingFake) Send(text string) error       { f.sent = append(f.sent, text); return nil }
func (f *escalatingFake) Interrupt() error             { return nil }
func (f *escalatingFake) Alive() bool                  { return true }
func (f *escalatingFake) TurnPhase() claudia.TurnPhase { return f.phase }
func (f *escalatingFake) SendEscalating(text string, esc escalate.Ladder) (claudia.DeliveryOutcome, error) {
	f.ladders = append(f.ladders, esc)
	if f.err != nil {
		return claudia.DeliveryOutcome{}, f.err
	}
	return claudia.DeliveryOutcome{Mechanism: "stub_steer", PhaseBefore: claudia.TurnInTurn}, nil
}

// plainFake cannot run a ladder.
type plainFake struct{ phase claudia.TurnPhase }

func (f *plainFake) Send(string) error            { return nil }
func (f *plainFake) Interrupt() error             { return nil }
func (f *plainFake) Alive() bool                  { return true }
func (f *plainFake) TurnPhase() claudia.TurnPhase { return f.phase }

// 🎯T899: who is sending decides whether a busy seat is pressed.
func TestT899EscalationClass(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	if got := s.escalationClass("", OriginOwner, RelationOwnerSurface); got != config.EscalationOwner {
		t.Fatalf("owner = %q", got)
	}
	if got := s.escalationClass("jevons", OriginAgent, RelationDirectDown); got != config.EscalationOverseer {
		t.Fatalf("overseer directing down = %q", got)
	}
	for _, c := range []struct {
		actor string
		rel   DeliverRelation
	}{{"jv-t1-worker", RelationReportUp}, {"jevons-po", RelationDirectDown}, {"ge-po", RelationPeer}} {
		if got := s.escalationClass(c.actor, OriginAgent, c.rel); got != "" {
			t.Fatalf("%s (%s) = %q; reports, peers and POs directing down wait for the turn", c.actor, c.rel, got)
		}
	}
}

// A busy seat gets the sender's ladder; an idle one, a seat that cannot run
// a ladder, or one that cannot steer take the ordinary path.
func TestT899EscalateIfBusy(t *testing.T) {
	s := New(t.TempDir(), nil, nil)

	busy := &escalatingFake{phase: claudia.TurnInTurn}
	res, handled, err := observedEscalateIfBusy(s, "jevons-po", "status?", config.EscalationOwner, "jevons", busy)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	want := escalate.Ladder{{Mode: claudia.DeliverySteer}, {Mode: claudia.DeliveryInterrupt, After: config.DefaultOwnerInterruptAfter}}
	if len(busy.ladders) != 1 || len(busy.ladders[0]) != 2 || busy.ladders[0][0] != want[0] || busy.ladders[0][1] != want[1] {
		t.Fatalf("ladder = %+v, want %+v", busy.ladders, want)
	}
	if res.Status != "steered" || !strings.Contains(res.Message, "interrupted after 1m0s") || len(busy.sent) != 0 {
		t.Fatalf("res = %+v, sent = %v", res, busy.sent)
	}

	if _, handled, _ := observedEscalateIfBusy(s, "jevons-po", "hi", config.EscalationOwner, "jevons", &escalatingFake{phase: claudia.TurnIdle}); handled {
		t.Fatal("an idle seat takes the ordinary submit")
	}
	if _, handled, _ := observedEscalateIfBusy(s, "jevons-po", "hi", "", "jevons", busy); handled {
		t.Fatal("a sender without a profile waits for the turn")
	}
	if _, handled, _ := observedEscalateIfBusy(s, "jevons-po", "hi", config.EscalationOwner, "jevons", &plainFake{phase: claudia.TurnInTurn}); handled {
		t.Fatal("a seat that cannot run a ladder takes the ordinary path")
	}
	noSteer := &escalatingFake{phase: claudia.TurnInTurn, err: claudia.ErrSteerUnsupported}
	if _, handled, err := observedEscalateIfBusy(s, "jevons-po", "hi", config.EscalationOwner, "jevons", noSteer); handled || err != nil {
		t.Fatalf("steer unsupported: handled=%v err=%v; nothing reached the seat, so the ordinary path holds it", handled, err)
	}

	s.SetDeliveryEscalation(config.DeliveryEscalationConfig{Overseer: config.EscalationProfile{InterruptAfterSeconds: 5}})
	busy.ladders = nil
	if _, handled, _ := observedEscalateIfBusy(s, "jevons-po", "go", config.EscalationOverseer, "jevons", busy); !handled ||
		len(busy.ladders) != 1 || busy.ladders[0][1].After != 5*time.Second {
		t.Fatalf("configured overseer ladder = %+v", busy.ladders)
	}
}

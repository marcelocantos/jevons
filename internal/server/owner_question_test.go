// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/converge"
)

// 🎯T378 live-path oracle. internal/ownerqa covers the predicate and the
// fixture-chatlog scan; this file covers the wiring, which is where the defect
// actually lived: the daemon credited an owner turn as replied-to on the
// *seal*, so ~100 consecutive turns that painted nothing still satisfied
// reply-or-residual and the owner's question evaporated in silence.

// questionServer is ownerHealthServer under its own name. It used to tighten
// the bounds so the oracle would not sit through the shipped 5-minute answer
// grace; with a controlled clock (🎯T437) waiting the grace out costs nothing,
// so these oracles run against the contract as shipped.
func questionServer(t *testing.T, send func(string) error) (*Server, chan string, *ownerClock) {
	t.Helper()
	return ownerHealthServer(t, send)
}

// sealEmptyTurn is one overseer turn that ends having painted nothing — the
// 019fe5e8 record, and the shape a [silent] reply also takes on the wire.
func sealEmptyTurn(s *Server) {
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
}

// sealVisibleTurn is an overseer turn that paints prose the owner can read.
func sealVisibleTurn(s *Server, text string) {
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: text})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
}

func questionGap(t *testing.T, s *Server) (converge.OwnerGap, bool) {
	t.Helper()
	for _, g := range s.OwnerHealthGaps() {
		if g.Dimension == converge.OwnerDimQuestionAnswered {
			return g, true
		}
	}
	return converge.OwnerGap{}, false
}

// The incident, end to end on the real wire. The owner asks; the overseer
// burns turn after turn producing nothing owner-visible. Every one of those
// turns seals, so the pre-🎯T378 daemon saw a perfectly healthy chat.
func TestOwnerQuestionUnansweredIsDetectedDespiteSeals(t *testing.T) {
	s, _, clk := questionServer(t, func(string) error { return nil })
	ownerSend(t, s, "Orthograph is giving Pippa a 127.0.0.1 address... haven't we integrated pigeon?")

	for range 5 {
		sealEmptyTurn(s)
	}

	s.ReconcileOwnerHealth(clk.Advance(time.Minute))

	// The point of the target, stated as an assertion: the *old* dimension is
	// satisfied. If this ever flips, this test stops proving anything.
	for _, g := range s.OwnerHealthGaps() {
		if g.Dimension == converge.OwnerDimReplyOrResidual {
			t.Fatalf("reply_or_residual opened a gap (%s) — the seal-credits-a-reply behaviour this target exists to compensate for has changed; re-derive the oracle", g.Kind)
		}
	}

	g, ok := questionGap(t, s)
	if !ok {
		t.Fatal("owner question went unanswered through 5 empty turns and no gap opened")
	}
	if g.Kind != converge.OwnerGapNoOpLoop {
		t.Errorf("kind = %q, want %q", g.Kind, converge.OwnerGapNoOpLoop)
	}
}

// Acceptance 2: detection must actually re-pressure. The pre-fix requeue
// actuator bailed whenever a reply had sealed — which is every turn of the
// incident — so this asserts the owner's own words reach the overseer again.
func TestOwnerQuestionGapRepressuresWithTheOwnersWords(t *testing.T) {
	sent := make(chan string, 16)
	s, _, clk := questionServer(t, func(text string) error {
		sent <- text
		return nil
	})
	const question = "haven't we integrated pigeon?"
	ownerSend(t, s, question)
	<-sent // the original delivery

	for range 5 {
		sealEmptyTurn(s)
	}
	s.ReconcileOwnerHealth(clk.Advance(time.Minute))

	var requeued bool
	for {
		select {
		case text := <-sent:
			if strings.Contains(text, question) {
				requeued = true
			}
			continue
		default:
		}
		break
	}
	if !requeued {
		t.Fatal("unanswered question was never put back in front of the overseer — the question can still age out of scrollback")
	}
}

// The over-broadness guard, on the live path. A routine directive answered
// [silent] is healthy supervision traffic: same empty seals, same silence, no
// gap. A detector that fails this fires all day and gets switched off.
func TestOwnerRoutineTurnAnsweredSilentIsNeverAGap(t *testing.T) {
	for _, turn := range []string{"continue", "ship it", "fix the build", "ok"} {
		t.Run(turn, func(t *testing.T) {
			s, _, clk := questionServer(t, func(string) error { return nil })
			ownerSend(t, s, turn)
			for range 10 {
				sealEmptyTurn(s)
			}
			s.ReconcileOwnerHealth(clk.Advance(time.Hour))
			if g, ok := questionGap(t, s); ok {
				t.Fatalf("routine turn %q opened %s — [silent] is legitimate for everything but a question", turn, g.Kind)
			}
		})
	}
}

// A question answered with owner-visible prose closes the dimension, even if
// the overseer went quiet for a while first.
func TestOwnerQuestionAnsweredByVisibleProse(t *testing.T) {
	s, _, clk := questionServer(t, func(string) error { return nil })
	ownerSend(t, s, "haven't we integrated pigeon?")

	sealEmptyTurn(s)
	sealEmptyTurn(s)
	sealVisibleTurn(s, "We have — Pippa needs re-pairing. Steps: …")

	s.ReconcileOwnerHealth(clk.Advance(time.Minute))
	if g, ok := questionGap(t, s); ok {
		t.Fatalf("answered question still opened %s (%s)", g.Kind, g.Reason)
	}
}

// A [silent] reply is not an answer. This is the distinction the whole target
// rests on: the same body that legitimately closes a routine turn must not
// close a question, because the owner never sees it.
func TestOwnerQuestionIsNotAnsweredBySilentReply(t *testing.T) {
	s, _, clk := questionServer(t, func(string) error { return nil })
	ownerSend(t, s, "haven't we integrated pigeon?")

	// A whole-stream [silent] ops reply: suppressed on the wire, sealed as an
	// empty end_turn. The owner sees nothing.
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "[silent] relayed to orthograph-po"})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	for range 4 {
		sealEmptyTurn(s)
	}

	s.ReconcileOwnerHealth(clk.Advance(time.Minute))
	if _, ok := questionGap(t, s); !ok {
		t.Fatal("a [silent] reply closed an owner question — the owner saw nothing, so the question is still unanswered")
	}
}

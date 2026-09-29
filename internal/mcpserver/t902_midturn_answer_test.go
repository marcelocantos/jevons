// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T902: a busy PO answers a steered question mid-turn and carries on. The
// asker gets that answer when the PO moves on to its next tool call, not at
// the end of the turn.
func TestT902MidTurnAnswerReachesAskerBeforeTurnEnd(t *testing.T) {
	s := &Server{}
	var mu sync.Mutex
	var got []string
	s.SetNotify(func(text string) { mu.Lock(); got = append(got, text); mu.Unlock() })
	received := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), got...) }

	const question = "status of T540?"
	s.midTurn().expect("jevons-po", s.overseerName(), question)
	sink := s.agentEventSink("jevons-po")

	// The PO is mid-turn on other work; its text before the absorb is not
	// the answer.
	sink(claudia.Event{Type: "assistant", Text: "Refactoring the loader.", StopReason: "tool_use"})
	sink(claudia.Event{Type: "progress", ProgressType: claudia.ProgressDeliveryAbsorbed, Text: question})
	sink(claudia.Event{Type: "assistant", Text: "T540 is waiting on "})
	sink(claudia.Event{Type: "assistant", Text: "the fidelity audit.", StopReason: "tool_use"})

	deadline := time.Now().Add(5 * time.Second)
	for len(received()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	r := received()
	if len(r) != 1 {
		t.Fatalf("asker got %d messages before the turn ended, want 1: %q", len(r), r)
	}
	if !strings.Contains(r[0], "T540 is waiting on the fidelity audit.") || !strings.Contains(r[0], "jevons-po") {
		t.Fatalf("mid-turn relay = %q", r[0])
	}
	if strings.Contains(r[0], "Refactoring the loader") {
		t.Fatalf("relay carried text from before the question was taken: %q", r[0])
	}

	// A second tool call does not relay again.
	sink(claudia.Event{Type: "assistant", Text: "More loader work.", StopReason: "tool_use"})
	time.Sleep(50 * time.Millisecond)
	if n := len(received()); n != 1 {
		t.Fatalf("relayed %d times; one answer, one relay", n)
	}
}

// When the answer is the last thing the turn says, the turn-end report
// carries it; nothing is relayed twice.
func TestT902AnswerAtTurnEndIsNotDuplicated(t *testing.T) {
	m := &midTurnAnswers{}
	m.expect("po", "jevons", "q")
	if _, _, ok := m.observe("po", claudia.Event{Type: "progress", ProgressType: claudia.ProgressDeliveryAbsorbed, Text: "q"}); ok {
		t.Fatal("absorb alone relayed")
	}
	if _, _, ok := m.observe("po", claudia.Event{Type: "assistant", Text: "answer", StopReason: "end_turn"}); ok {
		t.Fatal("a turn-ending answer was relayed; the turn-end report carries it")
	}
	// The capture is gone: a later turn's tool call relays nothing.
	m.observe("po", claudia.Event{Type: "assistant", Text: "next turn"})
	if _, _, ok := m.observe("po", claudia.Event{Type: "assistant", StopReason: "tool_use"}); ok {
		t.Fatal("capture outlived its turn")
	}
}

// Tool calls before the model has said anything are its route to the
// answer, not the answer; an absorb of someone else's text starts nothing.
func TestT902CaptureWaitsForText(t *testing.T) {
	m := &midTurnAnswers{}
	m.expect("po", "jevons", "q")
	m.observe("po", claudia.Event{Type: "progress", ProgressType: claudia.ProgressDeliveryAbsorbed, Text: "other"})
	m.observe("po", claudia.Event{Type: "assistant", Text: "unrelated"})
	if _, _, ok := m.observe("po", claudia.Event{Type: "assistant", StopReason: "tool_use"}); ok {
		t.Fatal("an absorb of different text started a capture")
	}
	m.observe("po", claudia.Event{Type: "progress", ProgressType: claudia.ProgressDeliveryAbsorbed, Text: "q"})
	if _, _, ok := m.observe("po", claudia.Event{Type: "progress", ProgressType: claudia.ProgressToolUse}); ok {
		t.Fatal("a tool call with no answer yet relayed")
	}
	m.observe("po", claudia.Event{Type: "assistant", Text: "it is done"})
	asker, answer, ok := m.observe("po", claudia.Event{Type: "progress", ProgressType: claudia.ProgressToolUse})
	if !ok || asker != "jevons" || answer != "it is done" {
		t.Fatalf("relay = %q %q %v", asker, answer, ok)
	}
}

// A failed send leaves no question waiting; a stale one expires.
func TestT902ForgetAndExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	m := &midTurnAnswers{now: func() time.Time { return now }}
	m.expect("po", "jevons", "q")
	m.forget("po", "q")
	m.observe("po", claudia.Event{Type: "progress", ProgressType: claudia.ProgressDeliveryAbsorbed, Text: "q"})
	m.observe("po", claudia.Event{Type: "assistant", Text: "x"})
	if _, _, ok := m.observe("po", claudia.Event{Type: "progress", ProgressType: claudia.ProgressToolUse}); ok {
		t.Fatal("forgotten question relayed")
	}
	m.expect("po", "jevons", "old")
	now = now.Add(midTurnTTL + time.Second)
	m.expect("po", "jevons", "new")
	if n := len(m.pending["po"]); n != 1 {
		t.Fatalf("pending = %d, want the stale question expired", n)
	}
}

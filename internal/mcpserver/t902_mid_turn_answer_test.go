// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T902: a steered mid-turn answer reaches its asker as soon as the agent
// gives it — via the next tool_use pause — not only with the turn-end
// report. Reproduces the T899.1 shape: the overseer steers a status
// question into a busy jevons-po, jevons-po answers, then keeps working.
func TestT902MidTurnAnswerRelayedOnToolPause(t *testing.T) {
	s := &Server{}
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })

	busy := &escalatingFake{phase: claudia.TurnInTurn}
	res, handled, err := s.escalateIfBusy("jevons-po", "status?", config.EscalationOverseer, "jevons", busy)
	if err != nil || !handled || res.Status != "steered" {
		t.Fatalf("escalateIfBusy: res=%+v handled=%v err=%v", res, handled, err)
	}

	sink := s.agentEventSink("jevons-po")
	// The agent absorbs the steer and answers before its next tool call.
	sink(claudia.Event{Type: "assistant", Text: "I'm mid-survey, in the seat. Status: on track."})
	if len(delivered) != 0 {
		t.Fatalf("relayed before any tool_use boundary: %v", delivered)
	}
	// A tool_use pause is the signal that the answer is complete.
	sink(claudia.Event{Type: "assistant", StopReason: "tool_use"})
	if len(delivered) != 1 {
		t.Fatalf("want exactly one mid-turn relay at the tool_use pause, got %d: %v", len(delivered), delivered)
	}
	if !strings.Contains(delivered[0], "jevons-po") || !strings.Contains(delivered[0], "Status: on track") {
		t.Fatalf("mid-turn relay missing agent name or answer: %q", delivered[0])
	}

	// The agent continues and finishes its turn later; that terminal report
	// is a second, independent delivery — the mid-turn answer is not lost
	// nor duplicated into it.
	sink(claudia.Event{Type: "assistant", Text: "Both test runs passed.", StopReason: "end_turn"})
	if len(delivered) != 2 {
		t.Fatalf("want a second delivery for the turn-end report, got %d: %v", len(delivered), delivered)
	}
	if !strings.Contains(delivered[1], "Both test runs passed") {
		t.Fatalf("turn-end report missing the rest of the turn: %q", delivered[1])
	}
}

// A steer with no pending ask (ordinary submit/queue path) never triggers a
// mid-turn relay — only an actual escalated steer registers one.
func TestT902NoMidTurnRelayWithoutPendingAsk(t *testing.T) {
	s := &Server{}
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })

	sink := s.agentEventSink("jv-worker")
	sink(claudia.Event{Type: "assistant", Text: "working"})
	sink(claudia.Event{Type: "assistant", StopReason: "tool_use"})
	if len(delivered) != 0 {
		t.Fatalf("relayed with no pending ask registered: %v", delivered)
	}
}

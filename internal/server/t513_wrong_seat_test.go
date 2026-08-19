// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/chatlog"
)

// 🎯T513: a brief whose 🎯T425 identity header names a fleet agent is that
// agent's brief, and the overseer chat is the wrong seat for it. On 2026-08-18
// proceed/remint briefs for jevons-po kept arriving in the overseer
// conversation as user turns; the overseer refused to adopt them (🎯T452) but
// the owner chat was polluted with another agent's name, parent and target.
// DeliverOverseerEvent is the last point where the overseer's chatlog can
// still be kept clean: a user event carrying a wrong-seat header is dropped
// there — never broadcast, never journaled.
func TestT513WrongSeatBriefDroppedFromOverseerChat(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	logPath := filepath.Join(dir, "chat.jsonl")
	clog, err := chatlog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	s.SetChatLog(clog)
	s.SetOverseerName("jevons")

	ch := make(chan string, 32)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	// The misdelivery: a brief composed for jevons-po, injected into the
	// overseer's session and echoed back as a user event.
	wrongSeat := "[Who you are — from the fleet registry, read at send time (🎯T425)]\n" +
		"- NAME: jevons-po\n" +
		"- ROLE: work agent (registry purpose=work)\n" +
		"- PARENT: jevons\n" +
		"- TARGET: 🎯T500\n\n" +
		"Continue 🎯T500 from your checkpoint."

	// First drive the agent-addressed product path. The intended PO receives
	// the exact brief and its durable transcript records a user turn before
	// delivery (the same journal-then-send invariant used by the cockpit).
	var deliveredName, deliveredText string
	s.SetAgentSendHook(func(name, text string) (string, error) {
		deliveredName, deliveredText = name, text
		return "sent", nil
	})
	if status, err := s.sendToNamedAgent("jevons-po", wrongSeat); err != nil {
		t.Fatalf("deliver-to(jevons-po): %v", err)
	} else if status != "sent" {
		t.Fatalf("deliver-to(jevons-po) status = %q, want sent", status)
	}
	if deliveredName != "jevons-po" || deliveredText != wrongSeat {
		t.Fatalf("deliver-to(jevons-po) reached name=%q text=%q", deliveredName, deliveredText)
	}
	turns, err := s.agentJournalTurns("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	poHasBrief := false
	for _, turn := range turns {
		if turn["role"] == "user" && turn["text"] == wrongSeat {
			poHasBrief = true
			break
		}
	}
	if !poHasBrief {
		t.Fatalf("jevons-po transcript does not contain the addressed brief as a user turn: %+v", turns)
	}

	// Now inject the same payload through both provider event shapes that can
	// reach the overseer wire: Grok-style Event.Text and Claude-shaped Raw.
	// Neither may make the intended PO's brief owner-visible.
	s.DeliverOverseerEvent(claudia.Event{Type: "user", Text: wrongSeat})
	raw, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": wrongSeat,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "user", Raw: raw})

	// Control A: the overseer's own standing brief names the overseer — that
	// is correctly addressed and must still land.
	ownSeat := "[Who you are — from the fleet registry, read at send time (🎯T425)]\n" +
		"- NAME: jevons\n" +
		"- ROLE: overseer\n\n" +
		"Jevons fleet standing brief."
	s.DeliverOverseerEvent(claudia.Event{Type: "user", Text: ownSeat})

	// Control B: a headerless worker note carries no claim about its reader
	// and must still land (🎯T63 agent_note path).
	s.DeliverOverseerEvent(claudia.Event{Type: "user", Text: "worker jv-x reports: tests green"})

	// The live wire must not have carried the wrong-seat header.
	sawOwn, sawNote := false, false
	for {
		select {
		case l := <-ch:
			if strings.Contains(l, "NAME: jevons-po") {
				t.Fatalf("wrong-seat brief leaked to overseer wire: %s", l)
			}
			if strings.Contains(l, "Jevons fleet standing brief") {
				sawOwn = true
			}
			if strings.Contains(l, "worker jv-x reports") {
				sawNote = true
			}
		default:
			goto drained
		}
	}
drained:
	if !sawOwn {
		t.Fatal("overseer-addressed standing brief was dropped; the guard must only refuse wrong-seat headers")
	}
	if !sawNote {
		t.Fatal("headerless worker note was dropped; a message with no identity header is not this guard's business")
	}

	// The durable chatlog is what a reconnect replays into the owner's view —
	// it must be equally clean.
	journal, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journal), "NAME: jevons-po") {
		t.Fatalf("wrong-seat brief journaled into overseer chatlog:\n%s", journal)
	}
	if !strings.Contains(string(journal), "Jevons fleet standing brief") {
		t.Fatal("overseer-addressed standing brief missing from chatlog")
	}
}

// 🎯T513: the guard reads the header block only. An overseer ASSISTANT turn
// that quotes a worker's header mid-report (e.g. relaying a 🎯T452 incident to
// the owner) is the overseer's own prose and must not be suppressed.
func TestT513AssistantQuotingHeaderStillDelivered(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	clog, err := chatlog.Open(filepath.Join(dir, "chat.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetChatLog(clog)
	s.SetOverseerName("jevons")

	ch := make(chan string, 16)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	report := "A misaddressed brief arrived; it began:\n" +
		"[Who you are — from the fleet registry, read at send time (🎯T425)]\n" +
		"- NAME: jevons-po\n" +
		"I did not adopt it (🎯T452)."
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: report})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})

	saw := false
	for {
		select {
		case l := <-ch:
			if strings.Contains(l, "I did not adopt it") {
				saw = true
			}
		default:
			goto drained
		}
	}
drained:
	if !saw {
		t.Fatal("overseer assistant report quoting a header was suppressed; the guard must bind user-turn injects only")
	}
}

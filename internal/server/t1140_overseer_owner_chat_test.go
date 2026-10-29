// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
)

func ownerReplyDecision(payload string) string {
	return "```jevons\njevons: kind owner-reply\njevons: purpose decision\n```\n" + payload
}

func TestT1140RoutineOverseerTurnSuppressedOnOwnerWire(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 32)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.overseerOwnerTurn = false // fleet-triggered chew
	s.mu.Unlock()

	routine := "All routine post-restart status confirmations — consistent with T692. No action needed."
	for _, frag := range []string{routine[:20], routine[20:]} {
		s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: frag})
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "", StopReason: "end_turn"})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case l := <-ch:
			if strings.Contains(l, "No action needed") || strings.Contains(l, "routine post-restart") {
				t.Fatalf("routine overseer prose reached owner wire: %s", l)
			}
		case <-deadline:
			return
		}
	}
}

func TestT1140ClassifyOwnerReplyShow(t *testing.T) {
	msg := ownerReplyDecision("Two open decisions need you before Friday.")
	if envelope.ClassifyOverseerOwnerStream(msg, true) != envelope.OverseerOwnerShow {
		t.Fatalf("want show at terminal")
	}
}

func TestT1140OwnerReplyChatWireLine(t *testing.T) {
	msg := ownerReplyDecision("Two open decisions need you before Friday.")
	line, ok := chatWireLine(claudia.Event{Type: "assistant", Text: msg, StopReason: "end_turn"})
	if !ok || !strings.Contains(line, "Two open decisions") {
		t.Fatalf("chatWireLine ok=%v line=%s", ok, line)
	}
}

func TestT1140OwnerReplyDecisionReachesOwnerWire(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 32)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.mu.Unlock()

	msg := ownerReplyDecision("Two open decisions need you before Friday.")
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: msg})
	s.mu.Lock()
	accAfterFirst := s.overseerStreamAcc
	silentAfterFirst := s.overseerStreamSilent
	s.mu.Unlock()
	if accAfterFirst == "" {
		t.Fatal("stream accumulator empty after first fragment")
	}
	if silentAfterFirst {
		t.Fatal("first owner-reply fragment marked silent stream")
	}
	if envelope.ClassifyOverseerOwnerStream(accAfterFirst, true) != envelope.OverseerOwnerShow {
		t.Fatalf("want show after first fragment, got %v", envelope.ClassifyOverseerOwnerStream(accAfterFirst, true))
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "", StopReason: "end_turn"})

	deadline := time.After(2 * time.Second)
	var seen []string
	for {
		select {
		case l := <-ch:
			seen = append(seen, l)
			if strings.Contains(l, "Two open decisions") {
				if strings.Contains(l, "jevons: kind") {
					t.Fatalf("envelope leaked on wire: %s", l)
				}
				return
			}
		case <-deadline:
			t.Fatalf("owner-facing owner-reply never reached wire; saw %v", seen)
		}
	}
}

func TestT1140OwnerTurnUnenvelopedReachesOwnerWire(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 32)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.overseerOwnerTurn = true
	s.mu.Unlock()

	reply := "The fleet has twelve registered seats right now."
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: reply})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "", StopReason: "end_turn"})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case l := <-ch:
			if strings.Contains(l, "twelve registered seats") {
				return
			}
		case <-deadline:
			t.Fatal("unenveloped owner-turn reply never reached owner wire")
		}
	}
}

func TestT1140OwnerReplyAnswerPurposeShow(t *testing.T) {
	msg := "```jevons\njevons: kind owner-reply\njevons: purpose answer\n```\nYes — the gate is GREEN."
	if envelope.ClassifyOverseerOwnerStream(msg, true) != envelope.OverseerOwnerShow {
		t.Fatalf("want show for purpose answer")
	}
}

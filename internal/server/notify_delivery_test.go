// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"strings"
	"testing"
)

// 🎯T62: a worker reply that arrives while the overseer is mid-turn must be
// queued and delivered when the turn completes — never dropped on "prompt
// already in flight" (the silent loss that stranded jevons-po's replies:
// notify fired, proc.Send failed, the note was gone).
func TestOverseerNotesQueueAndRetryWhenBusy(t *testing.T) {
	s := &Server{}
	var delivered []string
	busy := true
	s.notifySender = func(text string) error {
		if busy {
			return fmt.Errorf("grok acp: prompt already in flight")
		}
		delivered = append(delivered, text)
		return nil
	}

	// Two notes arrive while the overseer is busy — both queued, none dropped.
	_ = s.SendToOverseer("[Agent a responded]\nfoo")
	_ = s.SendToOverseer("[Agent b responded]\nbar")
	if len(delivered) != 0 {
		t.Fatalf("delivered while overseer busy: %v", delivered)
	}

	// Overseer turn completes → drain delivers the coalesced batch once.
	busy = false
	s.drainOverseerNotes() // simulates HandleAgentEvent's terminal-stop drain
	if len(delivered) != 1 {
		t.Fatalf("want one coalesced delivery, got %d: %v", len(delivered), delivered)
	}
	if !strings.Contains(delivered[0], "foo") || !strings.Contains(delivered[0], "bar") {
		t.Fatalf("coalesced note lost content: %q", delivered[0])
	}

	// Nothing left to resend.
	s.drainOverseerNotes()
	if len(delivered) != 1 {
		t.Fatalf("re-drain resent an already-delivered note: %v", delivered)
	}
}

// When the overseer is idle, a note delivers immediately — no turn-complete
// event needed.
func TestOverseerNoteDeliversWhenIdle(t *testing.T) {
	s := &Server{}
	var delivered []string
	s.notifySender = func(text string) error { delivered = append(delivered, text); return nil }
	_ = s.SendToOverseer("[Agent x responded]\nhi")
	if len(delivered) != 1 || !strings.Contains(delivered[0], "hi") {
		t.Fatalf("idle note not delivered immediately: %v", delivered)
	}
}

// A transient failure keeps the note queued; the very next drain redelivers
// it intact (no loss, no duplication).
func TestOverseerNoteSurvivesTransientFailure(t *testing.T) {
	s := &Server{}
	var delivered []string
	fail := true
	s.notifySender = func(text string) error {
		if fail {
			fail = false
			return fmt.Errorf("grok acp: prompt already in flight")
		}
		delivered = append(delivered, text)
		return nil
	}
	_ = s.SendToOverseer("[Agent y responded]\nkeep me")
	if len(delivered) != 0 {
		t.Fatalf("delivered despite first-attempt failure: %v", delivered)
	}
	s.drainOverseerNotes()
	if len(delivered) != 1 || !strings.Contains(delivered[0], "keep me") {
		t.Fatalf("note not redelivered after transient failure: %v", delivered)
	}
}

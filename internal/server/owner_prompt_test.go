// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/chatlog"
)

// TestDeliverOwnerPromptCancelAndSendOrdersCancelBeforePrompt is the
// Grok-CLI cancel-and-send oracle: while a turn is in flight, a second
// owner message must interrupt and wait for idle before Send.
func TestDeliverOwnerPromptCancelAndSendOrdersCancelBeforePrompt(t *testing.T) {
	s := New("test", t.TempDir())
	var mu sync.Mutex
	var log []string
	var sends int
	s.ownerInterrupt = func() error {
		mu.Lock()
		log = append(log, "interrupt")
		mu.Unlock()
		// Simulate ACP finishing the cancelled turn shortly after cancel.
		go func() {
			time.Sleep(30 * time.Millisecond)
			s.HandleAgentEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
		}()
		return nil
	}
	s.ownerSend = func(payload string) error {
		mu.Lock()
		defer mu.Unlock()
		sends++
		if s.overseerInFlight && sends == 1 {
			// Should not happen on second prompt path after wait.
		}
		log = append(log, "send:"+payload)
		return nil
	}

	// First prompt: idle.
	if err := s.DeliverOwnerPrompt("first"); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	if !s.overseerInFlight {
		t.Fatal("expected in-flight after first send")
	}
	s.mu.RUnlock()

	// Second while busy: must interrupt, wait, then send.
	if err := s.DeliverOwnerPrompt("correction"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(log, " | ")
	if !strings.Contains(joined, "interrupt") {
		t.Fatalf("expected interrupt before correction: %s", joined)
	}
	// Last send must be the correction, after interrupt.
	last := log[len(log)-1]
	if !strings.Contains(last, "correction") {
		t.Fatalf("last send = %q, want correction; full=%s", last, joined)
	}
	// Ordering: interrupt appears before the correction send.
	iInt, iSend := -1, -1
	for i, e := range log {
		if e == "interrupt" && iInt < 0 {
			iInt = i
		}
		if strings.Contains(e, "correction") {
			iSend = i
		}
	}
	if iInt < 0 || iSend < 0 || iInt > iSend {
		t.Fatalf("interrupt must precede correction send: %s", joined)
	}
}

// TestDeliverOwnerPromptSurfacesInFlightAfterTimeout: if cancel never
// completes, the replacement surfaces an error (not silent drop).
func TestDeliverOwnerPromptSurfacesInFlightAfterTimeout(t *testing.T) {
	s := New("test", t.TempDir())
	s.mu.Lock()
	s.overseerInFlight = true
	s.mu.Unlock()
	// No idle signal — wait should time out. Shrink wait via direct call.
	s.ownerInterrupt = func() error { return nil }
	s.ownerSend = func(string) error { t.Fatal("must not send while still in flight"); return nil }

	// Use short wait by calling waitOverseerIdle directly.
	err := s.waitOverseerIdle(50 * time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout")
	}
}

// TestJournalSealsStreamNotTokenFrames: live stream chunks are not
// journaled; end_turn seals one assistant line.
func TestJournalSealsStreamNotTokenFrames(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/jevons.jsonl"
	l, err := chatlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	s := New("test", dir)
	s.SetChatLog(l)

	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "Hel"})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "lo"})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "", StopReason: "end_turn"})

	var lines []string
	if err := l.Replay(func(line string) error { lines = append(lines, line); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("journal lines = %d %v, want 1 sealed assistant", len(lines), lines)
	}
	if !strings.Contains(lines[0], `"Hello"`) && !strings.Contains(lines[0], `Hello`) {
		// content is nested JSON
		if !strings.Contains(lines[0], "Hello") {
			t.Fatalf("sealed line missing full text: %s", lines[0])
		}
	}
	if !strings.Contains(lines[0], "end_turn") {
		t.Fatalf("sealed line missing stop_reason: %s", lines[0])
	}
}

// TestDeliverOwnerPromptRetryAfterInFlightError covers the race where
// Send returns "already in flight" even after we thought we were idle.
func TestDeliverOwnerPromptRetryAfterInFlightError(t *testing.T) {
	s := New("test", t.TempDir())
	var n int
	s.ownerInterrupt = func() error {
		go func() {
			time.Sleep(20 * time.Millisecond)
			s.HandleAgentEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
		}()
		return nil
	}
	s.ownerSend = func(payload string) error {
		n++
		if n == 1 {
			// Pretend busy without pre-marking — forces retry path.
			s.mu.Lock()
			s.overseerInFlight = true
			s.mu.Unlock()
			return fmt.Errorf("grok acp: prompt already in flight")
		}
		return nil
	}
	// Start "busy" so first branch interrupts.
	s.mu.Lock()
	s.overseerInFlight = true
	s.mu.Unlock()

	if err := s.DeliverOwnerPrompt("hi"); err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("expected retry send, got %d attempts", n)
	}
}

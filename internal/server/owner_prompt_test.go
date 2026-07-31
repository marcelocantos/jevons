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
// completes, DeliverOwnerPrompt force-clears the local idle gate and
// still attempts Send via the real entry point (Grok-CLI best-effort
// cancel-and-send). If Send then fails with "already in flight", that
// error is returned — never a silent drop.
func TestDeliverOwnerPromptSurfacesInFlightAfterTimeout(t *testing.T) {
	s := New("test", t.TempDir())
	s.ownerIdleWait = 40 * time.Millisecond
	s.mu.Lock()
	s.overseerInFlight = true
	s.mu.Unlock()
	// Interrupt acknowledged but ACP never signals idle.
	s.ownerInterrupt = func() error { return nil }

	// Case A: after force-idle, Send succeeds.
	sends := 0
	s.ownerSend = func(payload string) error {
		sends++
		if !strings.Contains(payload, "correction after stuck turn") {
			t.Fatalf("unexpected payload %q", payload)
		}
		return nil
	}
	if err := s.DeliverOwnerPrompt("correction after stuck turn"); err != nil {
		t.Fatalf("expected force-idle then send to succeed: %v", err)
	}
	if sends != 1 {
		t.Fatalf("sends=%d want 1", sends)
	}

	// Case B: after force-idle, Send still fails — error must surface.
	s.mu.Lock()
	s.overseerInFlight = true
	s.mu.Unlock()
	sends = 0
	s.ownerSend = func(string) error {
		sends++
		return fmt.Errorf("grok acp: prompt already in flight")
	}
	err := s.DeliverOwnerPrompt("second correction")
	if err == nil {
		t.Fatal("expected error when Send remains in flight after force-idle")
	}
	if !strings.Contains(err.Error(), "already in flight") {
		t.Fatalf("error = %v, want already in flight", err)
	}
	if sends < 1 {
		t.Fatal("DeliverOwnerPrompt must attempt Send (not silent drop)")
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

// TestForceOverseerIdleBroadcastsCancelSettled so the UI can disarm
// suppressNextWorkingClear when ACP never emits cancel end_turn.
func TestForceOverseerIdleBroadcastsCancelSettled(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 4)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.overseerInFlight = true
	s.mu.Unlock()

	s.forceOverseerIdle()

	select {
	case line := <-ch:
		if !strings.Contains(line, "cancel_settled") {
			t.Fatalf("expected cancel_settled frame, got %s", line)
		}
	case <-time.After(time.Second):
		t.Fatal("no cancel_settled broadcast")
	}
	s.mu.RLock()
	if s.overseerInFlight {
		t.Fatal("still in flight after force")
	}
	s.mu.RUnlock()
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

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/chatlog"
)

// 🎯T210: rewind + cockpit re-attach must not stack DeliverOverseerEvent
// subscribers. Owner repro journaled every token twice (GotGot / CheckingChecking).
func TestAttachOverseerIdempotentNoDoubleBroadcast(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	logPath := filepath.Join(dir, "chat.jsonl")
	clog, err := chatlog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	s.SetChatLog(clog)

	ch := make(chan string, 32)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	// Bare agent — SubscribeEvents lazy-inits the map (no Start/PTY).
	agent := &claudia.Agent{}

	// Simulate rewind AttachOverseer + cockpit re-attach on the same process.
	s.AttachOverseer(agent)
	s.AttachOverseer(agent)
	if n := agent.EventSubscriberCount(); n != 1 {
		t.Fatalf("after double AttachOverseer, subscribers=%d want 1", n)
	}

	agent.PublishEvent(claudia.Event{
		Type: "assistant",
		Text: "Got",
	})
	agent.PublishEvent(claudia.Event{
		Type: "assistant",
		Text: " it",
	})
	agent.PublishEvent(claudia.Event{
		Type:       "assistant",
		StopReason: "end_turn",
	})

	// Drain live fan-out: exactly one wire line per published event.
	var lines []string
	deadline := time.After(2 * time.Second)
	for len(lines) < 3 {
		select {
		case l := <-ch:
			if isPhaseFrame(l) {
				continue // 🎯T555.1 phase chrome, not a bubble
			}
			lines = append(lines, l)
		case <-deadline:
			t.Fatalf("timeout waiting for 3 wire lines; got %d: %v", len(lines), lines)
		}
	}
	// No extras.
	time.Sleep(50 * time.Millisecond)
	drainPhaseFrames(t, ch) // any extra bubble line is a double broadcast

	if len(lines) != 3 {
		t.Fatalf("wire lines=%d want 3 (one per event, not doubled)", len(lines))
	}

	// Reconstruct sealed assistant text the way the UI bare-concats tokens.
	var sealed string
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "assistant" {
			t.Fatalf("type=%v line=%s", m["type"], line)
		}
		msg, _ := m["message"].(map[string]any)
		content, _ := msg["content"].([]any)
		for _, c := range content {
			block, _ := c.(map[string]any)
			if block["type"] == "text" {
				if t, ok := block["text"].(string); ok {
					sealed += t
				}
			}
		}
	}
	if sealed != "Got it" {
		t.Fatalf("sealed assistant text %q want %q (no GotGot doubling)", sealed, "Got it")
	}

	// Durable journal matches live: one crumb per event (rewind→resubmit paint path).
	var journal []string
	if err := clog.Replay(func(line string) error {
		journal = append(journal, line)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(journal) != 3 {
		t.Fatalf("journal lines=%d want 3; %v", len(journal), journal)
	}
}

// Double attach then swap to a fresh agent (rewind rotate) still fans once.
func TestAttachOverseerSwapAgentUnsubsOld(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 16)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	old := &claudia.Agent{}
	next := &claudia.Agent{}
	s.AttachOverseer(old)
	s.AttachOverseer(next)

	// Events on the stopped/replaced agent must not reach chat.
	old.PublishEvent(claudia.Event{Type: "assistant", Text: "stale"})
	select {
	case l := <-ch:
		t.Fatalf("old agent still subscribed after swap: %s", l)
	case <-time.After(30 * time.Millisecond):
	}

	next.PublishEvent(claudia.Event{Type: "assistant", Text: "fresh"})
	select {
	case l := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		msg, _ := m["message"].(map[string]any)
		content, _ := msg["content"].([]any)
		if len(content) != 1 {
			t.Fatalf("content=%v", content)
		}
		block, _ := content[0].(map[string]any)
		if block["text"] != "fresh" {
			t.Fatalf("text=%v", block["text"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for fresh agent event")
	}
}

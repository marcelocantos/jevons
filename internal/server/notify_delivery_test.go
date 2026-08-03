// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"log/slog"
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

func notifyAttrsMap(r slog.Record) map[string]any {
	m := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	return m
}

func findNotifyDecision(records []slog.Record, decision string) (map[string]any, bool) {
	for _, r := range records {
		if r.Message != "notify_queue" || r.Level != slog.LevelInfo {
			continue
		}
		attrs := notifyAttrsMap(r)
		if attrs["decision"] == decision {
			return attrs, true
		}
	}
	return nil, false
}

func intAttr(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	default:
		return 0, false
	}
}

// 🎯T128.3: busy-defer is Info (not Debug) with depth + err_class; re-queue
// path remains hermetically covered.
func TestOverseerNotifyBusyDeferLogsAtInfo(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

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

	_ = s.SendToOverseer("[Agent a responded]\nfoo")
	if len(delivered) != 0 {
		t.Fatalf("delivered while busy: %v", delivered)
	}

	enqueue, ok := findNotifyDecision(cap.records, "enqueue")
	if !ok {
		t.Fatal("expected Info notify_queue decision=enqueue")
	}
	if enqueue["component"] != "notify_queue" {
		t.Fatalf("enqueue component=%v", enqueue["component"])
	}
	if d, ok := intAttr(enqueue["depth"]); !ok || d < 1 {
		t.Fatalf("enqueue depth=%v", enqueue["depth"])
	}

	deferAttrs, ok := findNotifyDecision(cap.records, "defer")
	if !ok {
		t.Fatal("expected Info notify_queue decision=defer (busy path must not be Debug-only)")
	}
	if deferAttrs["component"] != "notify_queue" {
		t.Fatalf("defer component=%v", deferAttrs["component"])
	}
	if deferAttrs["err_class"] != "busy" {
		t.Fatalf("err_class=%v want busy", deferAttrs["err_class"])
	}
	depth, ok := intAttr(deferAttrs["depth"])
	if !ok || depth < 1 {
		t.Fatalf("defer depth=%v want >=1", deferAttrs["depth"])
	}
	deferred, ok := intAttr(deferAttrs["deferred"])
	if !ok || deferred < 1 {
		t.Fatalf("deferred=%v want >=1", deferAttrs["deferred"])
	}

	// Drain re-queue: notes still pending, then idle drain delivers once.
	if got := len(s.notifyQueue); got != 1 {
		t.Fatalf("queue depth after defer=%d want 1", got)
	}
	busy = false
	before := len(cap.records)
	s.drainOverseerNotes()
	if len(delivered) != 1 || !strings.Contains(delivered[0], "foo") {
		t.Fatalf("re-queue drain failed: delivered=%v", delivered)
	}
	drainAttrs, ok := findNotifyDecision(cap.records[before:], "drain")
	if !ok {
		t.Fatal("expected Info notify_queue decision=drain after successful re-delivery")
	}
	if drained, ok := intAttr(drainAttrs["drained"]); !ok || drained != 1 {
		t.Fatalf("drained=%v want 1", drainAttrs["drained"])
	}
}

func TestNotifyErrClass(t *testing.T) {
	if got := notifyErrClass(fmt.Errorf("grok acp: prompt already in flight")); got != "busy" {
		t.Fatalf("got %q", got)
	}
	if got := notifyErrClass(fmt.Errorf("overseer not running")); got != "not_running" {
		t.Fatalf("got %q", got)
	}
	if got := notifyErrClass(fmt.Errorf("connection reset")); got != "other" {
		t.Fatalf("got %q", got)
	}
	if got := notifyErrClass(nil); got != "" {
		t.Fatalf("nil got %q", got)
	}
}

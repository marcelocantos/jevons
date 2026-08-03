// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/transcript"
)

func TestInspectLiveEventUserAndAssistant(t *testing.T) {
	ev, ok := inspectLiveEvent(claudia.Event{Type: "user", Text: "do the thing"})
	if !ok || ev["type"] != "user" {
		t.Fatalf("user: ok=%v ev=%v", ok, ev)
	}
	msg, _ := ev["message"].(map[string]any)
	if msg["content"] != "do the thing" {
		t.Fatalf("user content=%v", msg)
	}

	ev, ok = inspectLiveEvent(claudia.Event{Type: "assistant", Text: "working"})
	if !ok || ev["type"] != "assistant" {
		t.Fatalf("assistant: ok=%v ev=%v", ok, ev)
	}

	ev, ok = inspectLiveEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	if !ok {
		t.Fatal("terminal stop should yield event")
	}
	msg, _ = ev["message"].(map[string]any)
	if msg["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason=%v", msg)
	}

	if _, ok := inspectLiveEvent(claudia.Event{Type: "progress", ProgressType: "tool_use"}); ok {
		t.Fatal("progress should not be inspect live text")
	}
}

func TestDeliverInspectLiveFansToSubscriber(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	ch := make(chan string, 8)
	s.setInspectSub(ch, "worker-a")

	s.DeliverInspectLive("worker-a", claudia.Event{Type: "assistant", Text: "hello"})

	select {
	case line := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "agent_transcript" || m["kind"] != inspectKindLive || m["name"] != "worker-a" {
			t.Fatalf("frame=%v", m)
		}
		ev, _ := m["event"].(map[string]any)
		if ev["type"] != "assistant" {
			t.Fatalf("event=%v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inspect live frame")
	}

	// Unrelated agent must not fan.
	s.DeliverInspectLive("other", claudia.Event{Type: "assistant", Text: "nope"})
	select {
	case line := <-ch:
		t.Fatalf("unexpected fan for other agent: %s", line)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMarshalAgentTranscriptHistoryShape(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name:      "worker-x",
		WorkDir:   dir,
		SessionID: "019fc2aa-bbbb-7ccc-8ddd-eeeeeeeeeeee",
		Purpose:   claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReader(filepath.Join(dir, "sessions")))

	line, ok := s.marshalAgentTranscriptHistory("worker-x")
	if !ok {
		t.Fatal("expected ok")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	if m["type"] != "agent_transcript" || m["kind"] != inspectKindHistory {
		t.Fatalf("envelope=%v", m)
	}
	if m["name"] != "worker-x" {
		t.Fatalf("name=%v", m["name"])
	}
	// Soft empty (no session file yet).
	if m["empty"] != true {
		t.Fatalf("want empty soft history, got %v", m)
	}
}

func TestSetInspectSubReplaceAndClear(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 1)
	s.setInspectSub(ch, "a")
	if !s.inspectHasSubscribers("a") {
		t.Fatal("want a subscribed")
	}
	s.setInspectSub(ch, "b")
	if s.inspectHasSubscribers("a") {
		t.Fatal("a should be cleared on replace")
	}
	if !s.inspectHasSubscribers("b") {
		t.Fatal("want b subscribed")
	}
	s.clearInspectSub(ch)
	if s.inspectHasSubscribers("b") {
		t.Fatal("b should be cleared")
	}
}

func TestOverseerInspectHistoryDenied(t *testing.T) {
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	payload, ok := s.buildAgentTranscriptPayload("jevons")
	if !ok {
		t.Fatal("ok")
	}
	if payload["denied"] != true {
		t.Fatalf("want denied, got %v", payload)
	}
}

// 🎯T209: control frames must be recognised by JSON "type", not byte prefix
// order — {"name":"x","type":"inspect_subscribe"} must not become owner chat.
func TestInspectControlFrameTypeOrderIndependent(t *testing.T) {
	// Simulate the fragile HasPrefix failure mode the daily probe hit.
	nameFirst := `{"name":"jv-t209-probe","type":"inspect_subscribe"}`
	typeFirst := `{"type":"inspect_subscribe","name":"jv-t209-probe"}`
	for _, raw := range []string{nameFirst, typeFirst} {
		var ctl struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(raw), &ctl); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if ctl.Type != "inspect_subscribe" || ctl.Name != "jv-t209-probe" {
			t.Fatalf("ctl=%+v from %s", ctl, raw)
		}
	}
	// Prefix match would only catch type-first — document the bug class.
	if strings.HasPrefix(nameFirst, `{"type":"inspect_subscribe"`) {
		t.Fatal("test assumption failed: name-first should not HasPrefix type-first")
	}
	if !strings.HasPrefix(typeFirst, `{"type":"inspect_subscribe"`) {
		t.Fatal("test assumption failed: type-first should HasPrefix")
	}
}

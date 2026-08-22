// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"
)

func TestInspectLiveEventUserAndAssistant(t *testing.T) {
	ev, ok := inspectLiveEvent(claudia.Event{Type: "user", Text: "do the thing"})
	if !ok || ev["type"] != "user" {
		t.Fatalf("user: ok=%v ev=%v", ok, ev)
	}
	msg, _ := ev["message"].(map[string]any)
	// 🎯T384: the live user frame carries typed blocks, matching the assistant
	// frame below, so the sidebar's display model reads one shape for both.
	if userBlockText(msg["content"]) != "do the thing" {
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

	ev, ok = inspectLiveEvent(claudia.Event{
		Type:         "progress",
		ProgressType: "tool_use",
		Raw:          []byte(`{"sessionUpdate":"tool_call","title":"Read","rawInput":{"path":"x"}}`),
	})
	if !ok {
		t.Fatal("tool_use progress must be inspect live so apply can emit ⋯ n steps")
	}
	if ev["type"] != "assistant" {
		t.Fatalf("tool_use wire type=%v", ev["type"])
	}
	msg, _ = ev["message"].(map[string]any)
	content, _ := msg["content"].([]map[string]any)
	if len(content) == 0 {
		// json unmarshal of []map from []any
		raw, _ := msg["content"].([]any)
		if len(raw) == 0 {
			t.Fatalf("tool_use content=%v", msg["content"])
		}
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
		if m["type"] != "assistant" || m["name"] != "worker-a" {
			t.Fatalf("frame=%v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inspect live frame")
	}

	s.DeliverInspectLive("worker-a", claudia.Event{
		Type:         "progress",
		ProgressType: "tool_use",
		Raw:          []byte(`{"sessionUpdate":"tool_call","title":"Read","rawInput":{"path":"x"}}`),
	})
	select {
	case line := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		msg, _ := m["message"].(map[string]any)
		raw, _ := msg["content"].([]any)
		if len(raw) == 0 {
			t.Fatalf("expected tool_use live event, got %v", m)
		}
		blk, _ := raw[0].(map[string]any)
		if blk["type"] != "tool_use" {
			t.Fatalf("expected tool_use block, got %v", blk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tool_use inspect live frame")
	}

	// Unrelated agent must not fan.
	s.DeliverInspectLive("other", claudia.Event{Type: "assistant", Text: "nope"})
	select {
	case line := <-ch:
		t.Fatalf("unexpected fan for other agent: %s", line)
	case <-time.After(50 * time.Millisecond):
	}
}

type replayBuf struct{ frames []map[string]any }

func (b *replayBuf) Write(ctx context.Context, typ websocket.MessageType, data []byte) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	b.frames = append(b.frames, m)
	return nil
}

func inspectReplay(t *testing.T, s *Server, name string) []map[string]any {
	t.Helper()
	buf := &replayBuf{}
	if err := s.writeInspectReplay(context.Background(), buf, name); err != nil {
		t.Fatal(err)
	}
	return buf.frames
}

func replayRoleRows(frames []map[string]any) []string {
	var out []string
	for _, m := range frames {
		typ, _ := m["type"].(string)
		switch typ {
		case "conversation_reset":
			continue
		case "agent_note":
			text, _ := m["text"].(string)
			out = append(out, "agent_note: "+text)
		case "user", "assistant":
			msg, _ := m["message"].(map[string]any)
			text := ""
			if msg != nil {
				text = userBlockText(msg["content"])
			}
			if text == "" {
				continue
			}
			out = append(out, typ+": "+text)
		}
	}
	return out
}

func replayUserCount(frames []map[string]any) int {
	n := 0
	for _, m := range frames {
		if m["type"] == "user" {
			n++
		}
	}
	return n
}

func replayHasToolUse(frames []map[string]any, name string) bool {
	for _, m := range frames {
		if m["type"] != "assistant" {
			continue
		}
		msg, _ := m["message"].(map[string]any)
		if msg == nil {
			continue
		}
		raw, _ := msg["content"].([]any)
		if len(raw) == 0 {
			if blocks, ok := msg["content"].([]map[string]any); ok {
				for _, blk := range blocks {
					if blk["type"] == "tool_use" && (name == "" || blk["name"] == name) {
						return true
					}
				}
			}
			continue
		}
		for _, blk := range raw {
			b, _ := blk.(map[string]any)
			if b["type"] == "tool_use" && (name == "" || b["name"] == name) {
				return true
			}
		}
	}
	return false
}

func TestWriteInspectReplayResetThenLive(t *testing.T) {
	s := New("test", t.TempDir())
	buf := &replayBuf{}
	if err := s.writeInspectReplay(context.Background(), buf, "jv-missing"); err != nil {
		t.Fatal(err)
	}
	if len(buf.frames) < 1 {
		t.Fatal("want reset frame")
	}
	if buf.frames[0]["type"] != "conversation_reset" {
		t.Fatalf("first=%v", buf.frames[0])
	}
	if buf.frames[0]["name"] != "jv-missing" {
		t.Fatalf("name=%v", buf.frames[0]["name"])
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

// 🎯T309.2: the overseer is addressable on the inspect family — writeInspectReplay
// of "jevons" is conversation_reset + chatlog frames, not a dump refusal.
func TestOverseerInspectHistoryNotDenied(t *testing.T) {
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	frames := inspectReplay(t, s, "jevons")
	if len(frames) < 1 || frames[0]["type"] != "conversation_reset" {
		t.Fatalf("want conversation_reset, got %v", frames)
	}
	if frames[0]["name"] != "jevons" {
		t.Fatalf("name=%v", frames[0]["name"])
	}
	for _, m := range frames {
		if _, denied := m["denied"]; denied {
			t.Fatalf("overseer must not be refused: %v", m)
		}
		if m["type"] == "agent_transcript" {
			t.Fatalf("dump envelope must not appear: %v", m)
		}
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

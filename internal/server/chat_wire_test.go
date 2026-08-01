// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T64: tool_call with rawInput must surface args for activity strip.
func TestToolCallDetailRawInput(t *testing.T) {
	raw := []byte(`{"update":{"sessionUpdate":"tool_call","title":"search_tool","rawInput":{"query":"jevonsmcp agent list"}}}`)
	name, input := toolCallDetail(raw)
	if name != "search_tool" {
		t.Fatalf("name=%q", name)
	}
	if input == nil || input["query"] != "jevonsmcp agent list" {
		t.Fatalf("input=%v", input)
	}
	// Without rawInput, name still works (T63).
	raw2 := []byte(`{"update":{"sessionUpdate":"tool_call","title":"Bash"}}`)
	n2, in2 := toolCallDetail(raw2)
	if n2 != "Bash" || in2 != nil {
		t.Fatalf("name=%q input=%v", n2, in2)
	}
	// Full progress wire line includes input for UI summariseInput.
	line, ok := chatWireLine(claudia.Event{
		Type: "progress", ProgressType: "tool_use", Raw: raw,
	})
	if !ok || !strings.Contains(line, "search_tool") || !strings.Contains(line, "jevonsmcp agent list") {
		t.Fatalf("wire line missing tool+args: ok=%v line=%s", ok, line)
	}
}

func TestChatWireLineGrokACPShapes(t *testing.T) {
	// These Raws mirror what claudia's grok_acp.go puts on Event.Raw —
	// session/update params for chunks, bare stopReason for prompt end.
	acpChunkRaw := []byte(`{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}`)
	acpEndRaw := []byte(`{"stopReason":"end_turn"}`)
	acpUserRaw := []byte(`{"sessionId":"s1","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hi"}}}`)
	acpToolRaw := []byte(`{"sessionId":"s1","update":{"sessionUpdate":"tool_call","title":"Bash","kind":"execute"}}`)

	tests := []struct {
		name       string
		ev         claudia.Event
		wantOK     bool
		wantType   string
		wantText   string // substring in marshalled line, if non-empty
		wantStop   string
		wantNoType bool // Raw had no type; wire must introduce one
	}{
		{
			name:     "assistant text chunk from ACP",
			ev:       claudia.Event{Type: "assistant", Text: "pong", Raw: acpChunkRaw},
			wantOK:   true,
			wantType: "assistant",
			wantText: `"text":"pong"`,
		},
		{
			name:     "empty-text end_turn from ACP",
			ev:       claudia.Event{Type: "assistant", Text: "", StopReason: "end_turn", Raw: acpEndRaw},
			wantOK:   true,
			wantType: "assistant",
			wantStop: "end_turn",
		},
		{
			// A genuine owner turn (userTurnPrefix) echoed by ACP is dropped:
			// chatUserEcho already rendered the clean bubble (🎯T63).
			name:   "prefixed owner turn suppressed",
			ev:     claudia.Event{Type: "user", Text: userTurnPrefix + "hi", Raw: acpUserRaw},
			wantOK: false,
		},
		{
			// An unprefixed user-role turn is an injected notification →
			// agent_note (activity strip), never a bubble.
			name:     "unprefixed note becomes agent_note",
			ev:       claudia.Event{Type: "user", Text: "[Agent po responded]\nPONG", Raw: acpUserRaw},
			wantOK:   true,
			wantType: "agent_note",
			wantText: `"text":"[Agent po responded]`,
		},
		{
			name:     "progress tool_use shows real tool name",
			ev:       claudia.Event{Type: "progress", ProgressType: "tool_use", Raw: acpToolRaw},
			wantOK:   true,
			wantType: "assistant",
			wantText: `"name":"Bash"`,
		},
		{
			// tool_call_update status frames are skipped to avoid duplicate
			// activity rows for one tool call.
			name:   "tool_call_update skipped",
			ev:     claudia.Event{Type: "progress", ProgressType: "tool_use", Raw: []byte(`{"update":{"sessionUpdate":"tool_call_update","title":"Bash: ls"}}`)},
			wantOK: false,
		},
		{
			name:   "empty assistant non-terminal dropped",
			ev:     claudia.Event{Type: "assistant", Text: "", Raw: acpEndRaw},
			wantOK: false,
		},
		{
			name: "already Claude-shaped assistant passed through",
			ev: claudia.Event{
				Type: "assistant",
				Text: "hello",
				Raw:  []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn"}}`),
				StopReason: "end_turn",
			},
			wantOK:   true,
			wantType: "assistant",
			wantText: "hello",
		},
		{
			name: "Claude-shaped system pass-through",
			ev: claudia.Event{
				Type: "system",
				Raw:  []byte(`{"type":"system","subtype":"init"}`),
			},
			wantOK:   true,
			wantType: "system",
		},
		{
			name:   "unknown noise dropped",
			ev:     claudia.Event{Type: "unknown", Raw: []byte(`{"foo":1}`)},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, ok := chatWireLine(tt.ev)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v want %v; line=%q", ok, tt.wantOK, line)
			}
			if !tt.wantOK {
				return
			}
			var probe map[string]any
			if err := json.Unmarshal([]byte(line), &probe); err != nil {
				t.Fatalf("wire not JSON: %v\nline=%s", err, line)
			}
			if got, _ := probe["type"].(string); got != tt.wantType {
				t.Fatalf("type=%q want %q; line=%s", got, tt.wantType, line)
			}
			if tt.wantText != "" && !strings.Contains(line, tt.wantText) {
				t.Fatalf("line missing %q:\n%s", tt.wantText, line)
			}
			if tt.wantStop != "" {
				msg, _ := probe["message"].(map[string]any)
				if msg == nil {
					t.Fatalf("no message object: %s", line)
				}
				if got, _ := msg["stop_reason"].(string); got != tt.wantStop {
					t.Fatalf("stop_reason=%q want %q; line=%s", got, tt.wantStop, line)
				}
			}
			// Frontend contract: assistant text must live under message.content[].
			if tt.wantType == "assistant" && tt.wantText != "" && strings.Contains(tt.wantText, `"text":`) {
				msg, _ := probe["message"].(map[string]any)
				content, _ := msg["content"].([]any)
				if len(content) == 0 {
					t.Fatalf("assistant content empty: %s", line)
				}
			}
		})
	}
}

func TestChatUserEcho(t *testing.T) {
	line := chatUserEcho("hello world")
	var probe map[string]any
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		t.Fatal(err)
	}
	if probe["type"] != "user" {
		t.Fatalf("type=%v", probe["type"])
	}
	msg, _ := probe["message"].(map[string]any)
	if msg["content"] != "hello world" {
		t.Fatalf("content=%v", msg["content"])
	}
}

func TestDeliverOverseerEventBroadcastsUIShape(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 16)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.mu.Unlock()

	// Simulate a full Grok turn: chunk then empty end_turn.
	s.DeliverOverseerEvent(claudia.Event{
		Type: "assistant",
		Text: "Hello",
		Raw:  []byte(`{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hello"}}}`),
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		Text:       "",
		StopReason: "end_turn",
		Raw:        []byte(`{"stopReason":"end_turn"}`),
	})

	var lines []string
	deadline := time.After(2 * time.Second)
	for len(lines) < 2 {
		select {
		case l := <-ch:
			lines = append(lines, l)
		case <-deadline:
			t.Fatalf("timeout waiting for wire lines; got %v", lines)
		}
	}

	// First: assistant with text content array.
	var a1 map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &a1); err != nil {
		t.Fatal(err)
	}
	if a1["type"] != "assistant" {
		t.Fatalf("line0 type=%v line=%s", a1["type"], lines[0])
	}
	msg1, _ := a1["message"].(map[string]any)
	content1, _ := msg1["content"].([]any)
	if len(content1) != 1 {
		t.Fatalf("line0 content=%v", content1)
	}
	block, _ := content1[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "Hello" {
		t.Fatalf("line0 block=%v", block)
	}

	// Second: terminal empty-content assistant with stop_reason.
	var a2 map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &a2); err != nil {
		t.Fatal(err)
	}
	if a2["type"] != "assistant" {
		t.Fatalf("line1 type=%v line=%s", a2["type"], lines[1])
	}
	msg2, _ := a2["message"].(map[string]any)
	if msg2["stop_reason"] != "end_turn" {
		t.Fatalf("line1 stop_reason=%v", msg2["stop_reason"])
	}

	// Idle status: waiting must clear.
	s.mu.RLock()
	waiting := s.waiting
	s.mu.RUnlock()
	if waiting {
		t.Fatal("waiting still true after terminal end_turn")
	}
}

func TestHandleAgentEventTerminalStopClearsWaiting(t *testing.T) {
	s := New("test", t.TempDir())
	s.mu.Lock()
	s.waiting = true
	s.turnBuf = "partial"
	s.mu.Unlock()

	s.HandleAgentEvent(claudia.Event{
		Type:       "assistant",
		Text:       "",
		StopReason: "end_turn",
	})

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.waiting {
		t.Fatal("waiting not cleared on terminal stop")
	}
	if s.turnBuf != "" {
		t.Fatalf("turnBuf=%q, want empty", s.turnBuf)
	}
}

func TestHandleAgentEventToolUseDoesNotClearWaiting(t *testing.T) {
	s := New("test", t.TempDir())
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()

	s.HandleAgentEvent(claudia.Event{
		Type:       "assistant",
		Text:       "calling tool",
		StopReason: "tool_use",
	})

	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.waiting {
		t.Fatal("waiting cleared on tool_use; turn still in flight")
	}
	if s.turnBuf != "calling tool" {
		t.Fatalf("turnBuf=%q", s.turnBuf)
	}
}

// TestUIContractAssistantWire documents the exact fields the browser
// handle() reads. If this breaks, the working indicator stays stuck.
func TestUIContractAssistantWire(t *testing.T) {
	line, ok := chatWireLine(claudia.Event{
		Type: "assistant",
		Text: "pong",
	})
	if !ok {
		t.Fatal("expected wire line")
	}
	// Mimic the browser's extraction path.
	var m struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StopReason string `json:"stop_reason"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != "assistant" {
		t.Fatalf("type=%q", m.Type)
	}
	hasText := false
	for _, c := range m.Message.Content {
		if c.Type == "text" && c.Text != "" {
			hasText = true
		}
	}
	if !hasText {
		t.Fatalf("UI would skip this line (no text content): %s", line)
	}
}

func TestUIContractEndTurnClearsWithoutText(t *testing.T) {
	line, ok := chatWireLine(claudia.Event{
		Type:       "assistant",
		StopReason: "end_turn",
	})
	if !ok {
		t.Fatal("end_turn must produce a wire line so the UI can clear working")
	}
	var m struct {
		Type    string `json:"type"`
		Message struct {
			Content    []any  `json:"content"`
			StopReason string `json:"stop_reason"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != "assistant" || m.Message.StopReason != "end_turn" {
		t.Fatalf("UI cannot detect terminal stop: %s", line)
	}
	// Browser rule: terminal || (hasText && !stop) → setWorking(false)
	terminal := m.Message.StopReason == "end_turn"
	if !terminal {
		t.Fatal("not terminal")
	}
}

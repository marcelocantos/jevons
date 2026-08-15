// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/chatlog"
)

// 🎯T64: product path is claudia handleSessionUpdate → Event.Raw → chatWireLine.
// Fixed claudia passes ACP session/update params through as Raw (preserves
// rawInput). This fixture is that real shape — not a hand-built partial.
func TestToolCallDetailFromClaudiaPreservedParams(t *testing.T) {
	// Exact ACP session/update params body (what fixed claudia puts on Event.Raw).
	acpParams := []byte(`{
		"sessionId":"s1",
		"update":{
			"sessionUpdate":"tool_call",
			"title":"search_tool",
			"kind":"other",
			"rawInput":{"query":"jevonsmcp agent list","limit":3}
		}
	}`)
	// Prove the OLD narrow re-marshal would have dropped rawInput (regression
	// guard): if someone reintroduces struct re-marshal without rawInput, this
	// documents the failure mode. We assert the product path uses full params.
	var narrow struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string `json:"sessionUpdate"`
			Title         string `json:"title"`
			Kind          string `json:"kind"`
		} `json:"update"`
	}
	if err := json.Unmarshal(acpParams, &narrow); err != nil {
		t.Fatal(err)
	}
	stripped, _ := json.Marshal(narrow)
	if strings.Contains(string(stripped), "rawInput") {
		t.Fatal("test assumption failed: narrow re-marshal unexpectedly kept rawInput")
	}
	_, strippedIn := toolCallDetail(stripped)
	if strippedIn != nil {
		t.Fatal("narrow re-marshal must lose input — if not, oracle is weak")
	}

	// Product path: Event as emitted by fixed claudia (Raw = full params).
	ev := claudia.Event{Type: "progress", ProgressType: "tool_use", Raw: acpParams}
	name, input := toolCallDetail(ev.Raw)
	if name != "search_tool" {
		t.Fatalf("name=%q", name)
	}
	if input == nil || input["query"] != "jevonsmcp agent list" {
		t.Fatalf("input=%v want query from rawInput", input)
	}
	line, ok := chatWireLine(ev)
	if !ok || !strings.Contains(line, "search_tool") || !strings.Contains(line, "jevonsmcp agent list") {
		t.Fatalf("wire line missing tool+args: ok=%v line=%s", ok, line)
	}
}

func TestChatWireLineGrokACPShapes(t *testing.T) {
	// These Raws mirror what claudia's grok_acp.go puts on Event.Raw —
	// session/update params for chunks, bare stopReason for prompt end.
	acpChunkRaw := []byte(`{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}`)
	acpEndRaw := []byte(`{"stopReason":"end_turn"}`)
	// 🎯T455: the frame claudia's publishPromptResult emits when session/prompt
	// comes back a JSON-RPC error — the backend's own answer, not agent prose.
	acpErrorRaw := []byte(`{"code":-32603,"message":"Internal error"}`)
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
			// 🎯T237: bare Internal error is rewritten with class, not left as-is.
			// 🎯T455: and it is the error FRAME that says so — Raw is the JSON-RPC
			// error the backend returned, which is the evidence being classified.
			name:     "internal error classified on wire",
			ev:       claudia.Event{Type: "assistant", Text: "Internal error", StopReason: "end_turn", Raw: acpErrorRaw},
			wantOK:   true,
			wantType: "assistant",
			wantText: `backend_unavailable`,
		},
		{
			// 🎯T455: the same words with no error frame behind them are an
			// agent talking about an outage. Verbatim, no banner.
			name:     "internal error as authored prose stays verbatim",
			ev:       claudia.Event{Type: "assistant", Text: "Internal error", StopReason: "end_turn", Raw: acpEndRaw},
			wantOK:   true,
			wantType: "assistant",
			wantText: `"text":"Internal error"`,
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
				Type:       "assistant",
				Text:       "hello",
				Raw:        []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn"}}`),
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
	// 🎯T384: the owner echo carries typed blocks, not a bare string, so a
	// block-walking consumer can read the owner's own words.
	if userBlockText(msg["content"]) != "hello world" {
		t.Fatalf("content=%v", msg["content"])
	}
	if _, isString := msg["content"].(string); isString {
		t.Fatal("owner echo regressed to the bare-string shape (🎯T384)")
	}
}

// 🎯T223: stampStreamID puts a join key on assistant wire frames.
func TestStampStreamID(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`
	got := stampStreamID(line, "s42")
	var probe map[string]any
	if err := json.Unmarshal([]byte(got), &probe); err != nil {
		t.Fatal(err)
	}
	if probe["stream_id"] != "s42" {
		t.Fatalf("stream_id=%v want s42; line=%s", probe["stream_id"], got)
	}
	if stampStreamID(line, "") != line {
		t.Fatal("empty id must leave line unchanged")
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

	// First: assistant with text content array + stream_id (🎯T223).
	var a1 map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &a1); err != nil {
		t.Fatal(err)
	}
	if a1["type"] != "assistant" {
		t.Fatalf("line0 type=%v line=%s", a1["type"], lines[0])
	}
	sid1, _ := a1["stream_id"].(string)
	if sid1 == "" {
		t.Fatalf("line0 missing stream_id: %s", lines[0])
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

	// Second: terminal empty-content assistant with stop_reason; same stream_id.
	var a2 map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &a2); err != nil {
		t.Fatal(err)
	}
	if a2["type"] != "assistant" {
		t.Fatalf("line1 type=%v line=%s", a2["type"], lines[1])
	}
	if a2["stream_id"] != sid1 {
		t.Fatalf("stream_id mismatch: chunk=%q end=%v", sid1, a2["stream_id"])
	}
	msg2, _ := a2["message"].(map[string]any)
	if msg2["stop_reason"] != "end_turn" {
		t.Fatalf("line1 stop_reason=%v", msg2["stop_reason"])
	}
	// Terminal clears open stream so the next turn mints a new id.
	s.mu.Lock()
	openID := s.overseerStreamID
	s.mu.Unlock()
	if openID != "" {
		t.Fatalf("overseerStreamID still %q after terminal", openID)
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

// 🎯T238: overseer [silent] assistant text must not become owner chat wire.
func TestChatWireLineSuppressesSilentAssistant(t *testing.T) {
	t.Parallel()
	line, ok := chatWireLine(claudia.Event{
		Type: "assistant",
		Text: "[silent] continued jv-t212; T222 already working.",
		Raw:  []byte(`{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"[silent] continued"}}}`),
	})
	if ok {
		t.Fatalf("silent assistant must not wire; got ok=true line=%s", line)
	}
	if strings.Contains(line, "[silent]") {
		t.Fatalf("silent text must not leak: %s", line)
	}

	// Case / trim still suppressed.
	_, ok = chatWireLine(claudia.Event{
		Type: "assistant",
		Text: "  [SILENT] ops ok",
	})
	if ok {
		t.Fatal("case-insensitive silent must suppress")
	}

	// Non-silent still delivered.
	line, ok = chatWireLine(claudia.Event{
		Type: "assistant",
		Text: "Owner needs this reply.",
	})
	if !ok || !strings.Contains(line, "Owner needs this reply") {
		t.Fatalf("non-silent assistant still delivered: ok=%v line=%s", ok, line)
	}
}

// 🎯T238: silent + terminal → empty end_turn (clear working), no silent body.
func TestChatWireLineSilentTerminalClearsWithoutBody(t *testing.T) {
	t.Parallel()
	line, ok := chatWireLine(claudia.Event{
		Type:       "assistant",
		Text:       "[silent] continued workers",
		StopReason: "end_turn",
	})
	if !ok {
		t.Fatal("silent terminal must still emit end_turn so UI clears working")
	}
	if strings.Contains(line, "[silent]") || strings.Contains(line, "continued") {
		t.Fatalf("silent body must be stripped: %s", line)
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
		t.Fatalf("want empty terminal assistant: %s", line)
	}
	if len(m.Message.Content) != 0 {
		t.Fatalf("content must be empty, got %v", m.Message.Content)
	}
}

// 🎯T242: multi-fragment visible + empty end_turn → journal coalesce keeps body.
func TestDeliverOverseerEventVisibleStreamEmptyEndTurnSealedBodyNonEmpty(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	ch := make(chan string, 32)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.mu.Unlock()

	for _, frag := range []string{"Hel", "lo owner", " stays."} {
		s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: frag})
	}
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		Text:       "",
		StopReason: "end_turn",
	})

	var lines []string
	deadline := time.After(2 * time.Second)
	for len(lines) < 4 {
		select {
		case l := <-ch:
			lines = append(lines, l)
		case <-deadline:
			t.Fatalf("timeout; lines=%v", lines)
		}
	}
	// Live wire must carry body fragments (flash path) and empty end_turn.
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Hel") || !strings.Contains(joined, " stays.") {
		t.Fatalf("live fragments missing: %v", lines)
	}

	// Seal+replay path: CoalesceStreamLines on journaled lines → non-empty body.
	sealed := chatlog.CoalesceStreamLines(lines)
	if len(sealed) != 1 {
		t.Fatalf("sealed count=%d want 1: %v", len(sealed), sealed)
	}
	if !strings.Contains(sealed[0], "Hello owner stays.") {
		t.Fatalf("sealed body empty/wiped: %s", sealed[0])
	}
	var m struct {
		Message struct {
			Content    []map[string]any `json:"content"`
			StopReason string           `json:"stop_reason"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(sealed[0]), &m); err != nil {
		t.Fatal(err)
	}
	if m.Message.StopReason != "end_turn" {
		t.Fatalf("stop=%q", m.Message.StopReason)
	}
	if len(m.Message.Content) == 0 {
		t.Fatalf("sealed content empty (T242 vanish): %s", sealed[0])
	}
}

// 🎯T240: multi-fragment stream "[silent]" + " continued" → no owner body.
func TestDeliverOverseerEventSuppressesMultiFragmentSilentStream(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 32)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.mu.Unlock()

	// Token-split silent turn (the T238 residual: later fragments lack prefix).
	for _, frag := range []string{"[silent]", " continued", " jv-t240"} {
		s.DeliverOverseerEvent(claudia.Event{
			Type: "assistant",
			Text: frag,
		})
	}
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		Text:       "",
		StopReason: "end_turn",
	})
	// Non-silent control.
	s.DeliverOverseerEvent(claudia.Event{
		Type: "assistant",
		Text: "Hello owner",
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		Text:       "",
		StopReason: "end_turn",
	})

	var lines []string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case l := <-ch:
			if strings.Contains(l, "continued") || strings.Contains(l, "[silent]") {
				t.Fatalf("silent fragment leaked: %s", l)
			}
			lines = append(lines, l)
			if strings.Contains(l, "Hello owner") {
				// wait one more for possible end_turn
				select {
				case l2 := <-ch:
					lines = append(lines, l2)
				case <-time.After(200 * time.Millisecond):
				}
				goto done
			}
		case <-deadline:
			t.Fatalf("timeout; lines=%v", lines)
		}
	}
done:
	sawHello := false
	sawEmptyTerminal := false
	for _, l := range lines {
		if strings.Contains(l, "Hello owner") {
			sawHello = true
		}
		if strings.Contains(l, `"stop_reason":"end_turn"`) && !strings.Contains(l, "Hello") {
			// empty content terminal for silent stream
			var m struct {
				Message struct {
					Content []any `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(l), &m) == nil && len(m.Message.Content) == 0 {
				sawEmptyTerminal = true
			}
		}
	}
	if !sawHello {
		t.Fatalf("non-silent missing: %v", lines)
	}
	if !sawEmptyTerminal {
		t.Fatalf("expected empty end_turn for silent stream among %v", lines)
	}
}

// 🎯T245: silent turn then agent_note then visible — live wire has no silent
// body; journal coalesce keeps only the visible owner reply body.
func TestDeliverOverseerEventSilentThenVisibleOnlySecondBody(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	ch := make(chan string, 64)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.mu.Unlock()

	// Turn A: pure silent.
	s.DeliverOverseerEvent(claudia.Event{
		Type: "assistant",
		Text: "[silent] PO already re-pressured jv-t244; no further action.",
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		Text:       "",
		StopReason: "end_turn",
	})
	// agent_note intervenes on the wire (owner-invisible).
	s.BroadcastChat(`{"type":"agent_note","text":"[Agent jevons-po responded] Independent gate"}`)
	// Turn B: visible.
	s.DeliverOverseerEvent(claudia.Event{
		Type: "assistant",
		Text: "**🎯T244 landed.**",
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		Text:       "",
		StopReason: "end_turn",
	})

	var lines []string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case l := <-ch:
			if strings.Contains(l, "[silent]") || strings.Contains(l, "re-pressured") {
				t.Fatalf("silent leaked on live wire: %s", l)
			}
			lines = append(lines, l)
			if strings.Contains(l, "T244 landed") {
				// drain short tail for end_turn
				select {
				case l2 := <-ch:
					lines = append(lines, l2)
				case <-time.After(200 * time.Millisecond):
				}
				goto done
			}
		case <-deadline:
			t.Fatalf("timeout; lines=%v", lines)
		}
	}
done:
	sealed := chatlog.CoalesceStreamLines(lines)
	// Expect agent_note + visible sealed (silent empty may also appear).
	var visibleBodies []string
	for _, ln := range sealed {
		if strings.Contains(ln, "re-pressured") || strings.Contains(ln, "[silent]") {
			t.Fatalf("silent in sealed view: %s", ln)
		}
		var m struct {
			Type    string `json:"type"`
			Message *struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(ln), &m) != nil {
			continue
		}
		if m.Type == "assistant" && m.Message != nil {
			for _, c := range m.Message.Content {
				if c.Text != "" {
					visibleBodies = append(visibleBodies, c.Text)
				}
			}
		}
	}
	if len(visibleBodies) != 1 || !strings.Contains(visibleBodies[0], "T244 landed") {
		t.Fatalf("want single visible body with T244 landed; got %v sealed=%v", visibleBodies, sealed)
	}
}

// 🎯T238: DeliverOverseerEvent → BroadcastChat/journal path drops silent body.
func TestDeliverOverseerEventSuppressesSilentAssistantJournal(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 16)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.mu.Unlock()

	s.DeliverOverseerEvent(claudia.Event{
		Type: "assistant",
		Text: "[silent] continued jv-x after worker-idle",
		Raw:  []byte(`{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"[silent] continued"}}}`),
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		Text:       "",
		StopReason: "end_turn",
		Raw:        []byte(`{"stopReason":"end_turn"}`),
	})

	// Non-silent control: must still deliver.
	s.DeliverOverseerEvent(claudia.Event{
		Type: "assistant",
		Text: "Hello owner",
		Raw:  []byte(`{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hello owner"}}}`),
	})

	var lines []string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case l := <-ch:
			if strings.Contains(l, "[silent]") {
				t.Fatalf("silent leaked to owner wire/journal path: %s", l)
			}
			lines = append(lines, l)
			if strings.Contains(l, "Hello owner") {
				goto done
			}
		case <-deadline:
			t.Fatalf("timeout; lines=%v", lines)
		}
	}
done:
	sawHello := false
	sawTerminal := false
	for _, l := range lines {
		if strings.Contains(l, "Hello owner") {
			sawHello = true
		}
		if strings.Contains(l, `"stop_reason":"end_turn"`) {
			sawTerminal = true
		}
	}
	if !sawHello {
		t.Fatalf("non-silent assistant missing: %v", lines)
	}
	if !sawTerminal {
		t.Fatalf("expected terminal end_turn frame among %v", lines)
	}
}

// 🎯T455: provider_failure on the chat wire is decided by the frame, not
// the words. Same refusal string: backend response records the event;
// authored content records nothing. A classifier that records nothing
// at all fails the transport arm; the pre-fix ClassifyText-only tree
// fails the authored arm.
func TestT455ChatWireRecordsTransportNotAuthored(t *testing.T) {
	t.Parallel()

	acpChunk := []byte(`{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Internal error"}}}`)
	acpError := []byte(`{"code":-32603,"message":"Internal error"}`)
	quoted := `The backend said "Internal error" but that was last hour; the fleet is working now.`
	incident := "**The whole fleet is dead — Claude account monthly spend limit reached.**"

	cases := []struct {
		name          string
		ev            claudia.Event
		wantClass     string // empty → no failure_class on the wire
		wantTextExact string
		wantTextSub   string
	}{
		{
			name: "Grok ACP JSON-RPC error frame is transport",
			ev: claudia.Event{
				Type:       "assistant",
				Text:       "Internal error",
				StopReason: "end_turn",
				Raw:        acpError,
			},
			wantClass:   "backend_unavailable",
			wantTextSub: "backend_unavailable",
		},
		{
			name: "Claude IsError flag is transport",
			ev: claudia.Event{
				Type:       "assistant",
				Text:       "Internal error",
				StopReason: "end_turn",
				IsError:    true,
			},
			wantClass:   "backend_unavailable",
			wantTextSub: "backend_unavailable",
		},
		{
			name: "ACP chunk with the same words is authored",
			ev: claudia.Event{
				Type:       "assistant",
				Text:       "Internal error",
				StopReason: "end_turn",
				Raw:        acpChunk,
			},
			wantTextExact: "Internal error",
		},
		{
			name: "quoted Internal error in a report is authored",
			ev: claudia.Event{
				Type: "assistant",
				Text: quoted,
				Raw:  acpChunk,
			},
			wantTextExact: quoted,
		},
		{
			name: "overseer spend-limit status is authored",
			ev: claudia.Event{
				Type: "assistant",
				Text: incident,
				Raw:  acpChunk,
			},
			wantTextExact: incident,
		},
		{
			name: "session/update that happens to nest code+message is authored",
			ev: claudia.Event{
				Type: "assistant",
				Text: "Internal error",
				Raw:  []byte(`{"sessionId":"s1","sessionUpdate":"agent_message_chunk","code":-32603,"message":"Internal error","update":{"sessionUpdate":"agent_message_chunk"}}`),
			},
			wantTextExact: "Internal error",
		},
	}

	sawTransportRecord := false
	sawAuthoredRefusal := false
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, ok := chatWireLine(tc.ev)
			if !ok {
				t.Fatal("expected a wire line")
			}
			var probe struct {
				FailureClass string `json:"failure_class"`
				Message      struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(line), &probe); err != nil {
				t.Fatalf("wire not JSON: %v\n%s", err, line)
			}
			var text string
			for _, c := range probe.Message.Content {
				if c.Type == "text" {
					text = c.Text
					break
				}
			}
			if probe.FailureClass != tc.wantClass {
				t.Fatalf("failure_class=%q want %q\n%s", probe.FailureClass, tc.wantClass, line)
			}
			if tc.wantTextExact != "" && text != tc.wantTextExact {
				t.Fatalf("text=%q want exact %q", text, tc.wantTextExact)
			}
			if tc.wantTextSub != "" && !strings.Contains(text, tc.wantTextSub) {
				t.Fatalf("text %q missing %q", text, tc.wantTextSub)
			}
			if tc.wantClass != "" {
				sawTransportRecord = true
			} else {
				sawAuthoredRefusal = true
			}
		})
	}
	if !sawTransportRecord {
		t.Fatal("over-broadness: at least one transport frame must record provider_failure")
	}
	if !sawAuthoredRefusal {
		t.Fatal("oracle too weak: at least one authored refusal must record nothing")
	}
}

func TestAssistantTextSource(t *testing.T) {
	t.Parallel()
	if got := assistantTextSource(claudia.Event{IsError: true}); got != agenterr.SourceTransport {
		t.Fatalf("IsError: %q", got)
	}
	if got := assistantTextSource(claudia.Event{Raw: []byte(`{"code":-32603,"message":"Internal error"}`)}); got != agenterr.SourceTransport {
		t.Fatalf("rpc error: %q", got)
	}
	if got := assistantTextSource(claudia.Event{Raw: []byte(`{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk"}}`)}); got != agenterr.SourceAuthored {
		t.Fatalf("session update: %q", got)
	}
	if got := assistantTextSource(claudia.Event{}); got != agenterr.SourceAuthored {
		t.Fatalf("empty: %q", got)
	}
}

func TestIsRPCErrorFrame(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want bool
	}{
		{`{"code":-32603,"message":"Internal error"}`, true},
		{`{"code":-32603,"message":"Internal error","data":{"retry":true}}`, true},
		{`{"message":"Internal error"}`, false},
		{`{"code":-32603}`, false},
		{`{"type":"assistant","code":-32603,"message":"Internal error"}`, false},
		{`{"sessionUpdate":"agent_message_chunk","code":-32603,"message":"x"}`, false},
		{`{"code":-32603,"message":"x","update":{"sessionUpdate":"agent_message_chunk"}}`, false},
		{``, false},
		{`not-json`, false},
	}
	for _, tc := range cases {
		if got := isRPCErrorFrame([]byte(tc.raw)); got != tc.want {
			t.Errorf("isRPCErrorFrame(%s)=%v want %v", tc.raw, got, tc.want)
		}
	}
}

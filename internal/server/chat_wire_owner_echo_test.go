// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T382 oracle. The owner sends one message; the provider then echoes the
// prompt it was handed, marker and all, back down the same event stream. The
// echo must never become a second bubble — under ANY provider.
//
// The fixture texts are the owner's real reported turn (screenshot
// 8fba5023741da729), including its pasted image ref, so a pass means his
// exact case is covered and not a sanitised stand-in.
const (
	ownerEchoBody  = "[image: 05f4f157043fa658]\nWhy is UI not rendering markdown in requests?"
	ownerEchoImage = "05f4f157043fa658"
)

// providerEcho is one backend's way of echoing the delivered prompt.
type providerEcho struct {
	provider string
	ev       claudia.Event
}

// ownerEchoStream is the turn stream for a single owner message: the clean
// bubble the browser already painted on send, then each provider's echo of
// the same words with userTurnPrefix still attached.
func ownerEchoStream() []providerEcho {
	delivered := userTurnPrefix + ownerEchoBody
	claudeRaw, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": delivered,
		},
	})
	if err != nil {
		panic(err)
	}
	// Codex/Anthropic block shape: same turn, content as typed text blocks.
	blockRaw, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]any{{"type": "text", "text": delivered}},
		},
	})
	if err != nil {
		panic(err)
	}
	return []providerEcho{
		{
			// Grok ACP: claudia synthesises Event.Text from the ACP chunk.
			provider: "grok",
			ev: claudia.Event{
				Type: "user",
				Text: delivered,
				Raw:  []byte(`{"sessionId":"s1","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"echo"}}}`),
			},
		},
		{
			// Claude: claudia fills Text for assistant events only, so the
			// turn's words arrive only in the Claude-shaped Raw line.
			provider: "claude",
			ev:       claudia.Event{Type: "user", Raw: claudeRaw},
		},
		{
			provider: "codex",
			ev:       claudia.Event{Type: "user", Raw: blockRaw},
		},
	}
}

// paintsOwnerBubble reports whether a wire line renders as an owner bubble —
// a user-role turn with prose content, which is what the browser paints.
func paintsOwnerBubble(t *testing.T, line string) bool {
	t.Helper()
	var probe struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		t.Fatalf("wire line is not JSON: %s", line)
	}
	if probe.Type != "user" || probe.Message.Role != "user" {
		return false
	}
	_, prose := userTurnText(claudia.Event{Raw: []byte(line)})
	return prose
}

// TestOwnerTurnPaintedOnceUnderEveryProvider is the T382 acceptance oracle:
// one owner message, one bubble, no marker, no doubled attachment — for grok,
// claude and codex alike.
func TestOwnerTurnPaintedOnceUnderEveryProvider(t *testing.T) {
	for _, echo := range ownerEchoStream() {
		t.Run(echo.provider, func(t *testing.T) {
			// The stream the client sees: the send-path echo, then the
			// provider's echo of the delivered prompt.
			stream := []string{chatUserEcho(ownerEchoBody)}
			if line, ok := chatWireLine(echo.ev); ok {
				stream = append(stream, line)
			}

			bubbles := 0
			images := 0
			for _, line := range stream {
				if paintsOwnerBubble(t, line) {
					bubbles++
				}
				if strings.Contains(line, userTurnPrefix) ||
					strings.Contains(line, strings.TrimSuffix(userTurnPrefix, "\n")) {
					t.Errorf("internal marker reached the transcript: %s", line)
				}
				images += strings.Count(line, ownerEchoImage)
			}
			if bubbles != 1 {
				t.Errorf("owner turn painted %d times, want exactly 1", bubbles)
			}
			if images != 1 {
				t.Errorf("attachment painted %d times, want exactly 1", images)
			}
		})
	}
}

// TestUserTurnTextLeavesStructuredLinesAlone guards the pass-through the fix
// narrows: tool_result and other non-prose user lines must still reach the UI
// verbatim, or Claude tool output disappears from the transcript.
func TestUserTurnTextLeavesStructuredLinesAlone(t *testing.T) {
	toolResult := `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"ok"}]}}`
	line, ok := chatWireLine(claudia.Event{Type: "user", Raw: []byte(toolResult)})
	if !ok || line != toolResult {
		t.Fatalf("tool_result line not passed through: ok=%v line=%s", ok, line)
	}
	if _, prose := userTurnText(claudia.Event{Raw: []byte(toolResult)}); prose {
		t.Error("tool_result content classified as prose")
	}
}

// TestEchoedOwnerTurnLineFiltersPreFixJournal covers the durable half: the
// owner's journal already holds echoes written before the fix, and both replay
// paths drop them so a reload does not repaint what live no longer emits.
func TestEchoedOwnerTurnLineFiltersPreFixJournal(t *testing.T) {
	// Verbatim shape of the poisoned record in the owner's own chatlog
	// (~/.jevons/chatlog/jevons.jsonl, the line after his clean bubble).
	poisoned, err := json.Marshal(map[string]any{
		"type":      "user",
		"timestamp": "2026-08-09T11:30:10.939Z",
		"message": map[string]any{
			"role":    "user",
			"content": userTurnPrefix + ownerEchoBody,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !echoedOwnerTurnLine(string(poisoned)) {
		t.Error("pre-fix owner echo not recognised; it would repaint on replay")
	}

	// Everything else in the journal must survive the filter.
	keep := []string{
		chatUserEcho(ownerEchoBody),
		`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"ok"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"[user] mentioned in prose"}],"stop_reason":"end_turn"}}`,
		`{"type":"agent_note","text":"[Agent po responded]\nPONG"}`,
		`{"type":"history_meta","older":0,"total":1}`,
	}
	for _, line := range keep {
		if echoedOwnerTurnLine(line) {
			t.Errorf("replay filter dropped a legitimate line: %s", line)
		}
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// userTurnPrefix marks a turn delivered to the overseer as a genuine owner
// message, distinguishing it from injected agent/system notifications
// (worker replies, budget alerts) that share the ACP "user" role. The
// browser renders prefixed turns as owner bubbles (prefix elided) and folds
// unprefixed notifications into the activity strip; the overseer itself sees
// the marker so it can tell the owner's words from a notification and relay
// only what the owner's instructions warrant (🎯T63).
const userTurnPrefix = "[user]\n"

// chatWireLine converts a claudia Event into the stable JSON line the
// web chat UI expects. Grok ACP events carry ACP session/update params
// (or a bare stopReason result) in Event.Raw — not the Claude-shaped
// type/message.content objects the frontend handle() understands.
// Without this normalisation, assistant text is dropped and the
// "Jevons is working…" indicator never clears.
//
// Returns ok=false when the event has nothing the UI should render
// (e.g. empty progress, unknown noise).
func chatWireLine(ev claudia.Event) (line string, ok bool) {
	ts := time.Now().UTC().Format(time.RFC3339Nano)

	switch ev.Type {
	case "user":
		if ev.Text == "" {
			// Pass through already-shaped Claude user lines (tool_result, etc.).
			if isClaudeShaped(ev.Raw) {
				return string(ev.Raw), true
			}
			return "", false
		}
		// A genuine owner turn carries userTurnPrefix. The browser already
		// rendered its clean bubble via chatUserEcho on send, so this ACP
		// echo is a duplicate — drop it (also de-dups the journal, which
		// used to store the owner turn twice: echo + ACP echo).
		if strings.HasPrefix(ev.Text, userTurnPrefix) {
			return "", false
		}
		// No marker → an injected agent/system notification (a worker reply,
		// a budget alert). The owner does not see these as chat; the overseer
		// decides what to relay. Surface it in the activity strip only, and
		// keep it in the journal for durability (🎯T63).
		b, err := json.Marshal(map[string]any{
			"type":      "agent_note",
			"timestamp": ts,
			"text":      ev.Text,
		})
		if err != nil {
			return "", false
		}
		return string(b), true

	case "assistant":
		if ev.Text != "" {
			msg := map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": ev.Text},
				},
			}
			if ev.StopReason != "" {
				msg["stop_reason"] = ev.StopReason
			}
			b, err := json.Marshal(map[string]any{
				"type":      "assistant",
				"timestamp": ts,
				"message":   msg,
			})
			if err != nil {
				return "", false
			}
			return string(b), true
		}
		// Empty-text terminal stop (Grok ACP session/prompt result).
		if ev.IsTerminalStop() {
			b, err := json.Marshal(map[string]any{
				"type":      "assistant",
				"timestamp": ts,
				"message": map[string]any{
					"role":        "assistant",
					"content":     []any{},
					"stop_reason": ev.StopReason,
				},
			})
			if err != nil {
				return "", false
			}
			return string(b), true
		}
		// Already Claude-shaped assistant (e.g. tool_use-only block).
		if isClaudeShaped(ev.Raw) {
			return string(ev.Raw), true
		}
		return "", false

	case "system":
		if isClaudeShaped(ev.Raw) {
			return string(ev.Raw), true
		}
		b, err := json.Marshal(map[string]any{
			"type":      "system",
			"timestamp": ts,
		})
		if err != nil {
			return "", false
		}
		return string(b), true

	case "progress":
		// Surface tool activity so the activity strip shows real work, not a
		// row of bare "tool_use:" (🎯T63). claudia collapses every ACP
		// tool_call/tool_call_update to ProgressType="tool_use"; the real
		// tool name and args live in ev.Raw. Emit one step per initiating
		// tool_call (skip the tool_call_update status frames, which would
		// otherwise stack duplicate rows).
		if ev.ProgressType == "" {
			return "", false
		}
		name, input := toolCallDetail(ev.Raw)
		if name == "" {
			return "", false
		}
		tu := map[string]any{"type": "tool_use", "name": name}
		if len(input) > 0 {
			tu["input"] = input
		}
		b, err := json.Marshal(map[string]any{
			"type":      "assistant",
			"timestamp": ts,
			"message": map[string]any{
				"role":    "assistant",
				"content": []map[string]any{tu},
			},
		})
		if err != nil {
			return "", false
		}
		return string(b), true

	default:
		if isClaudeShaped(ev.Raw) {
			return string(ev.Raw), true
		}
		return "", false
	}
}

// chatUserEcho builds the user-bubble wire line for a client-sent
// prompt. Grok ACP does not echo the prompt as a user event, so the
// chat handler synthesises one before forwarding to the overseer.
func chatUserEcho(text string) string {
	b, err := json.Marshal(map[string]any{
		"type":      "user",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"message": map[string]any{
			"role":    "user",
			"content": text,
		},
	})
	if err != nil {
		return `{"type":"user","message":{"role":"user","content":""}}`
	}
	return string(b)
}

// toolCallDetail pulls the human tool name and input args out of an ACP
// tool_call session/update (carried verbatim in a progress event's Raw).
// Returns an empty name for tool_call_update status frames and anything
// without a title, so the caller emits exactly one activity row per call
// instead of a stack of indistinguishable "tool_use:" rows (🎯T63).
func toolCallDetail(raw []byte) (name string, input map[string]any) {
	// claudia re-marshals the ACP update into Event.Raw with the update
	// object at top level (no JSON-RPC envelope) and, today, without
	// rawInput — so we can surface the tool name (title) but not its args
	// until claudia preserves rawInput. Still far better than "tool_use:".
	var probe struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			Title         string `json:"title"`
		} `json:"update"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", nil
	}
	if probe.Update.SessionUpdate != "tool_call" {
		return "", nil
	}
	return probe.Update.Title, nil
}

// isClaudeShaped reports whether raw already carries a top-level "type"
// the chat UI can dispatch on (user/assistant/system/tool_result/…).
func isClaudeShaped(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	switch probe.Type {
	case "user", "assistant", "system", "tool_result", "result", "rewound", "error", "progress":
		return true
	}
	return false
}

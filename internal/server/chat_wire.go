// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"time"

	"github.com/marcelocantos/claudia"
)

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
		b, err := json.Marshal(map[string]any{
			"type":      "user",
			"timestamp": ts,
			"message": map[string]any{
				"role":    "user",
				"content": ev.Text,
			},
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
		// Surface tool activity so the turn marker shows work in flight.
		if ev.ProgressType == "" {
			return "", false
		}
		b, err := json.Marshal(map[string]any{
			"type":      "assistant",
			"timestamp": ts,
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "tool_use", "name": ev.ProgressType, "input": map[string]any{}},
				},
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

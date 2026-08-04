// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/silentresponse"
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
			// 🎯T238 / 🎯T240: overseer ops replies marked [silent] must not
			// paint as owner-visible assistant bubbles. Single-fragment
			// Is() still applies here; multi-delta streams are suppressed
			// in DeliverOverseerEvent via accumulated Classify (stream seal).
			// Worker→overseer silent suppress remains on mcpserver notify.
			if silentresponse.Is(ev.Text) {
				// Terminal silent still needs end_turn so the UI clears working.
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
				return "", false
			}
			// 🎯T237: rewrite bare provider failure prose (e.g. "Internal error")
			// so owner-visible copy carries class; structured slog for T236.
			displayText := ev.Text
			var failClass agenterr.Class
			if class := agenterr.ClassifyText(ev.Text); class.IsFailure() {
				failClass = class
				displayText = agenterr.OwnerCopy(class, ev.Text)
				slog.Warn("provider_failure",
					"component", "provider_failure",
					"failure_class", class.String(),
					"transient", class.IsTransient(),
					"surface", "chat_wire",
					"raw", strings.TrimSpace(ev.Text),
				)
			}
			msg := map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": displayText},
				},
			}
			if ev.StopReason != "" {
				msg["stop_reason"] = ev.StopReason
			}
			wire := map[string]any{
				"type":      "assistant",
				"timestamp": ts,
				"message":   msg,
			}
			if failClass.IsFailure() {
				wire["failure_class"] = failClass.String()
			}
			b, err := json.Marshal(wire)
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

// stampStreamID injects stream_id onto a chat wire JSON object (🎯T223).
// Assistant (and related) fragments of one overseer response share one id
// so journal/history coalesce and the client join by identity, not adjacency.
// Empty id or invalid JSON leaves the line unchanged.
func stampStreamID(line, streamID string) string {
	streamID = strings.TrimSpace(streamID)
	if line == "" || streamID == "" {
		return line
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil || m == nil {
		return line
	}
	m["stream_id"] = streamID
	b, err := json.Marshal(m)
	if err != nil {
		return line
	}
	return string(b)
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
	// claudia re-marshals the ACP update into Event.Raw (update at top level
	// or nested). Prefer title + rawInput/arguments when present (🎯T64).
	var probe struct {
		SessionUpdate string          `json:"sessionUpdate"`
		Title         string          `json:"title"`
		RawInput      json.RawMessage `json:"rawInput"`
		Arguments     json.RawMessage `json:"arguments"`
		Update        struct {
			SessionUpdate string          `json:"sessionUpdate"`
			Title         string          `json:"title"`
			RawInput      json.RawMessage `json:"rawInput"`
			Arguments     json.RawMessage `json:"arguments"`
		} `json:"update"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", nil
	}
	su := probe.Update.SessionUpdate
	if su == "" {
		su = probe.SessionUpdate
	}
	if su != "tool_call" {
		return "", nil
	}
	name = probe.Update.Title
	if name == "" {
		name = probe.Title
	}
	input = decodeToolInput(probe.Update.RawInput)
	if input == nil {
		input = decodeToolInput(probe.Update.Arguments)
	}
	if input == nil {
		input = decodeToolInput(probe.RawInput)
	}
	if input == nil {
		input = decodeToolInput(probe.Arguments)
	}
	return name, input
}

func decodeToolInput(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil && len(m) > 0 {
		return m
	}
	// Sometimes rawInput is a JSON string containing an object.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		if err := json.Unmarshal([]byte(s), &m); err == nil && len(m) > 0 {
			return m
		}
	}
	return nil
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

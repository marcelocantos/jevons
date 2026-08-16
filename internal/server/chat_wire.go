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
// losslessEvent is the journal form of a provider event that chatWireLine
// or inspectLiveEvent would drop. Display ignores recorded=lossless.
func losslessEvent(ev claudia.Event) map[string]any {
	typ := ev.Type
	if typ == "" {
		typ = "unknown"
	}
	m := map[string]any{
		"type":      typ,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"recorded":  "lossless",
	}
	if ev.ProgressType != "" {
		m["progress_type"] = ev.ProgressType
	}
	if ev.Text != "" {
		m["text"] = ev.Text
	}
	if ev.StopReason != "" {
		m["stop_reason"] = ev.StopReason
	}
	if ev.Model != "" {
		m["model"] = ev.Model
	}
	if ev.IsError {
		m["is_error"] = true
	}
	if len(ev.Raw) > 0 {
		var raw any
		if err := json.Unmarshal(ev.Raw, &raw); err == nil {
			m["raw"] = raw
		} else {
			m["raw_text"] = string(ev.Raw)
		}
	}
	return m
}

func losslessLine(ev claudia.Event) string {
	b, err := json.Marshal(losslessEvent(ev))
	if err != nil {
		return `{"type":"unknown","recorded":"lossless"}`
	}
	return string(b)
}

func chatWireLine(ev claudia.Event) (line string, ok bool) {
	ts := time.Now().UTC().Format(time.RFC3339Nano)

	switch ev.Type {
	case "user":
		text, prose := userTurnText(ev)
		if !prose {
			// Pass through already-shaped Claude user lines (tool_result, etc.).
			if isClaudeShaped(ev.Raw) {
				return string(ev.Raw), true
			}
			return "", false
		}
		// A genuine owner turn carries userTurnPrefix. The browser already
		// rendered its clean bubble via chatUserEcho on send, so this provider
		// echo is a duplicate — drop it (also de-dups the journal, which
		// used to store the owner turn twice: echo + provider echo).
		if strings.HasPrefix(text, userTurnPrefix) {
			return "", false
		}
		// No marker → an injected agent/system notification (a worker reply,
		// a budget alert). The owner does not see these as chat; the overseer
		// decides what to relay. Surface it in the activity strip only, and
		// keep it in the journal for durability (🎯T63).
		b, err := json.Marshal(map[string]any{
			"type":      "agent_note",
			"timestamp": ts,
			"text":      text,
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
			// 🎯T455: only when the frame that carried it was the backend's own
			// answer — authored prose is never classified, so a sentence with
			// the word "failed" in it stays the sentence the agent wrote.
			displayText := ev.Text
			var failClass agenterr.Class
			if class := agenterr.ClassifyFrom(assistantTextSource(ev), ev.Text); class.IsFailure() {
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

// assistantTextSource reports which side of the wire an assistant event's text
// came from, so the classifier can refuse to read authored prose (🎯T455).
//
// Both arrive as Type "assistant" with the words on Event.Text, which is why
// classifying the text was ever plausible. The frames are not alike at all:
//
//   - A streamed word chunk is an ACP session/update — claudia's
//     handleSessionUpdate puts the update params on Raw. The agent composed
//     every character.
//   - A provider failure that kills the turn is the JSON-RPC error object the
//     backend returned to session/prompt — publishPromptResult marshals that
//     error onto Raw ({"code":…,"message":…}) and copies its message to Text.
//     No agent composed it; it is the answer the call got.
//
// Claude's transcript flags its own API failures instead (is_api_error_message
// / a top-level error string), which claudia surfaces as Event.IsError — an
// equally transport-level fact, and the reason this is not a Grok-only test.
//
// Anything else is authored. Fail-closed is deliberate and is the direction
// that matters: an unrecognised frame paints its text verbatim, which costs a
// missing banner. Guessing the other way costs a manufactured outage record in
// the signal 🎯T406/🎯T407 read to decide whether the fleet may work at all.
func assistantTextSource(ev claudia.Event) agenterr.Source {
	if ev.IsError {
		return agenterr.SourceTransport
	}
	if isRPCErrorFrame(ev.Raw) {
		return agenterr.SourceTransport
	}
	return agenterr.SourceAuthored
}

// isRPCErrorFrame reports whether raw is a bare JSON-RPC/ACP error object —
// a numeric code beside a message string, with none of the envelope a session
// update or a Claude-shaped transcript line carries. An agent cannot author
// this frame: it is written by the peer that refused the call.
func isRPCErrorFrame(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var probe struct {
		Code          *json.Number    `json:"code"`
		Message       *string         `json:"message"`
		Type          string          `json:"type"`
		SessionUpdate string          `json:"sessionUpdate"`
		Update        json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	if probe.Code == nil || probe.Message == nil {
		return false
	}
	// A session update or an already-shaped transcript line is content, even
	// when something nested inside it happens to be called "message".
	if probe.Type != "" || probe.SessionUpdate != "" || len(probe.Update) > 0 {
		return false
	}
	return true
}

// userTurnText extracts the prose of a user-role turn from a provider event,
// reporting prose=false for user lines that are not prose at all (tool_result
// blocks and other structured payloads the chat UI dispatches on directly).
//
// 🎯T382: the two providers deliver the same turn in different places.
// claudia's parseEvent fills Event.Text for assistant events only, so a Grok
// ACP user turn arrives with Text set (grok_acp.go synthesises it) while a
// Claude session-transcript user turn arrives with Text empty and its words
// only in the Claude-shaped Raw line. Reading both is what makes the paint
// decision below provider-agnostic: without it, the Claude echo of the
// prompt we just delivered skipped the userTurnPrefix check, fell into the
// Claude-shaped pass-through, and painted the owner's message a second time
// — attachments and the internal "[user]" marker included.
func userTurnText(ev claudia.Event) (text string, prose bool) {
	if ev.Text != "" {
		return ev.Text, true
	}
	var probe struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(ev.Raw, &probe); err != nil {
		return "", false
	}
	if probe.Type != "user" || len(probe.Message.Content) == 0 {
		return "", false
	}
	// Claude writes a plain owner turn as a bare string.
	var s string
	if err := json.Unmarshal(probe.Message.Content, &s); err == nil {
		if s == "" {
			return "", false
		}
		return s, true
	}
	// Typed blocks: prose only when every block is text. A single
	// tool_result block makes the line structured, not a turn to paint.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(probe.Message.Content, &blocks); err != nil || len(blocks) == 0 {
		return "", false
	}
	texts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type != "text" {
			return "", false
		}
		texts = append(texts, b.Text)
	}
	text = strings.Join(texts, "\n")
	if text == "" {
		return "", false
	}
	return text, true
}

// echoedOwnerTurnLine reports whether a stored chat wire line is a provider
// echo of an owner turn — a user-role line whose prose still carries the
// internal userTurnPrefix marker (🎯T382).
//
// Live traffic no longer produces these, but journals written before the fix
// contain them, and replaying one repaints the owner's message — its
// attachments and a literal "[user]" line with it. Both replay paths filter
// on this so history shows what live now emits.
func echoedOwnerTurnLine(line string) bool {
	text, prose := userTurnText(claudia.Event{Raw: []byte(line)})
	return prose && strings.HasPrefix(text, userTurnPrefix)
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

// wireTurnOriginKey names the provenance field carried on every user-role
// chat line (🎯T381). Its values are agentSendRequest.Origin's: "owner" for
// words the owner actually typed, "agent" for an injected agent/system report
// that shares the ACP user role (a worker seal report, a budget alert).
//
// The browser needs this because role alone cannot decide how a bubble paints.
// Owner turns must stay verbatim — an owner who types "**Commit:**" has to see
// "**Commit:**", not bold — while an agent report is a document and paints
// through the markdown renderer. Provenance is CARRIED, never sniffed: keying
// the paint off a regex over the body is exactly the mistake that would eat
// the owner's asterisks the first time he pastes a table.
const wireTurnOriginKey = "turn_origin"

// chatUserEcho builds the owner-bubble wire line for a client-sent
// prompt. Grok ACP does not echo the prompt as a user event, so the
// chat handler synthesises one before forwarding to the overseer.
func chatUserEcho(text string) string {
	return chatUserEchoAs(text, sendOriginOwner)
}

// chatUserEchoAs builds a user-role wire line stamped with who spoke it.
// Anything other than the agent origin is recorded as the owner: verbatim is
// the safe default, so a caller that has not been taught about provenance can
// never cause the owner's own words to be reinterpreted as formatting.
func chatUserEchoAs(text, origin string) string {
	if origin != sendOriginAgent {
		origin = sendOriginOwner
	}
	b, err := json.Marshal(map[string]any{
		"type":            "user",
		"timestamp":       time.Now().UTC().Format(time.RFC3339Nano),
		wireTurnOriginKey: origin,
		"message": map[string]any{
			"role": "user",
			// 🎯T384: typed blocks, the same shape every assistant turn uses.
			// This used to be a bare string, which made the owner's own words
			// the one thing in the journal a block-walking consumer could not
			// read — his message reached the agent, painted empty, and
			// vanished. Readers still accept the bare string (wireContentText,
			// userTurnText, ChatEvents.messageContentText) so every chatlog
			// already on disk keeps rendering.
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
		},
	})
	if err != nil {
		return `{"type":"user","message":{"role":"user","content":[]}}`
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

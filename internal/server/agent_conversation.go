// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T309.2: ONE agent-addressed conversation family. Three operations —
// transcript history, live subscribe, send — addressed uniformly by agent
// name, with the overseer as just another addressable agent:
//
//	history: ReplayTailSealed of the agent's journal (same as /ws/chat)
//	live:    named chat-wire frames on /ws/chat
//	send:    sendToNamedAgentAs(name, text, origin)
//
// Before this slice the overseer was refused by the inspect family (🎯T124
// "overseer uses main chat") and could only be reached through the /ws/chat
// owner wire, so three conversation operations existed that no agent-addressed
// call could perform. They now all resolve by name.
//
// TRANSPORT RESIDUAL (documented asymmetry, acceptance 2). What remains
// overseer-specific is transport and durability, not conversation capability:
//
//   - The overseer's durable record is the owner chat journal (chatlog), not a
//     provider session JSONL, so its turns are read from there. Both sources
//     project into the SAME turn shape ({turn_number, role, text}); the payload
//     names its origin in "source" (chatlog | session).
//   - GET /api/history + the /ws/chat replay-on-connect remain as the main
//     chat's paging/transport shim. They are a COMPAT SHIM over the same
//     journal, retained until the transport cutover in 🎯T10.6 (and the UI
//     cutover in 🎯T309.1) moves main chat onto the agent-addressed family.
//     No conversation operation is exclusive to them.
//   - A /ws/chat client subscribed to the overseer receives both the main chat
//     line and the agent_transcript live frame for the same event; de-duping
//     belongs to the single widget in 🎯T309.1.
//   - Conversation CONTROL ops (rewind, interrupt) sit outside this family on
//     both sides — the overseer's ride /ws/chat control frames, fleet agents'
//     ride MCP (jevons_transcript_rewind, jevons_agent_stop) and
//     POST /api/agents/engagement/stop. Unifying control is not this slice.
//
// Full write-up: docs/architecture-current.md § The conversation surface.
const (
	// conversationSourceSession marks turns read from a provider session
	// transcript (fleet agents).
	conversationSourceSession = "session"
	// conversationSourceChatlog marks turns read from the owner chat journal
	// (the overseer's durable conversation record).
	conversationSourceChatlog = "chatlog"
)

// overseerTranscriptMaxTurns caps how many recent overseer turns the family
// materialises in one history payload. The owner journal runs to 100k+ lines;
// older turns page through the /api/history compat shim (🎯T57) until 🎯T10.6.
const overseerTranscriptMaxTurns = 200

// overseerAgentName resolves the configured overseer registry name (🎯T44),
// falling back to the default. Must not be called while holding s.mu.
func (s *Server) overseerAgentName() string {
	if s == nil {
		return defaultOverseerName
	}
	s.mu.RLock()
	name := s.overseerName
	s.mu.RUnlock()
	if name == "" {
		return defaultOverseerName
	}
	return name
}

// isOverseerAgent reports whether name addresses the overseer.
func (s *Server) isOverseerAgent(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	return strings.EqualFold(name, s.overseerAgentName())
}

// buildOverseerTranscriptPayload serves the overseer's conversation history
// through the agent-addressed family, replacing the 🎯T124 refusal. Turns come
// from the owner chat journal in the same shape fleet transcripts use, so a
// caller addressing "jevons" gets a transcript exactly like any other agent.
func (s *Server) buildOverseerTranscriptPayload(name string) map[string]any {
	s.mu.RLock()
	clog := s.chatLog
	reg := s.registry
	s.mu.RUnlock()

	base := map[string]any{
		"type":    "agent_transcript",
		"kind":    inspectKindHistory,
		"name":    name,
		"purpose": "overseer",
		"source":  conversationSourceChatlog,
	}
	if reg != nil {
		for _, d := range reg.List() {
			if strings.EqualFold(d.Name, name) {
				base["session_id"] = d.SessionID
				base["workdir"] = d.WorkDir
				if d.Purpose != "" {
					base["purpose"] = d.Purpose
				}
				break
			}
		}
	}
	if clog == nil {
		s.logTranscriptEmpty(emptyReasonNoSession, name, "", "")
		base["turns"] = []any{}
		base["empty"] = true
		base["empty_reason"] = emptyReasonNoSession
		base["note"] = "no chat journal open"
		return base
	}

	var lines []string
	start, total, err := clog.ReplayTailSealed(overseerTranscriptMaxTurns, func(line string) error {
		lines = append(lines, line)
		return nil
	})
	var raw []string
	if _, _, rerr := clog.ReplayTailRaw(overseerTranscriptMaxTurns, func(line string) error {
		raw = append(raw, line)
		return nil
	}); rerr != nil {
		raw = nil
	}
	if err != nil {
		s.logTranscriptEmpty(emptyReasonReadError, name, "", err.Error())
		base["turns"] = []any{}
		base["empty"] = true
		base["empty_reason"] = emptyReasonReadError
		base["error"] = err.Error()
		return base
	}

	turns := overseerTurnsFromWire(lines)
	base["turns"] = turns
	if len(raw) > 0 {
		base["events"] = wireEventsFromLines(raw)
	} else {
		base["events"] = wireEventsFromLines(lines)
	}
	base["empty"] = len(turns) == 0
	// Paging parity with the /api/history compat shim: journal line indices,
	// not turn indices — a client fetching earlier windows uses these.
	base["start"] = start
	base["total"] = total
	if len(turns) == 0 {
		s.logTranscriptEmpty(emptyReasonZeroTurns, name, "", "")
		base["empty_reason"] = emptyReasonZeroTurns
	}
	return base
}

// overseerTurnsFromWire projects owner chat journal lines (chat wire frames)
// into the family's turn shape: one "user" row per owner turn, one "assistant"
// row carrying that turn's reply text, and "agent_note" rows for the injected
// worker/system notifications the overseer received during the turn.
//
// Mirrors internal/transcript extractTurns so both sources agree on shape:
// turns are numbered from 1 at each user boundary, and rows within a turn are
// ordered user → assistant → notes.
func overseerTurnsFromWire(lines []string) []map[string]any {
	var out []map[string]any
	turnNum := 0
	inTurn := false
	var userText, assistantText string
	var notes []string

	flush := func() {
		if !inTurn {
			return
		}
		out = append(out, map[string]any{
			"turn_number": turnNum,
			"role":        "user",
			"text":        userText,
		})
		if assistantText != "" {
			out = append(out, map[string]any{
				"turn_number": turnNum,
				"role":        "assistant",
				"text":        assistantText,
			})
		}
		for _, n := range notes {
			out = append(out, map[string]any{
				"turn_number": turnNum,
				"role":        "agent_note",
				"text":        n,
			})
		}
		assistantText = ""
		notes = nil
	}

	for _, ln := range lines {
		var frame struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ln), &frame); err != nil {
			continue
		}
		switch frame.Type {
		case "user":
			text := wireContentText(frame.Message.Content)
			// The ACP echo of an owner turn is prefixed; the clean echo the
			// browser sent is the journal's owner bubble (chat_wire.go).
			text = strings.TrimPrefix(text, userTurnPrefix)
			if strings.TrimSpace(text) == "" {
				continue
			}
			flush()
			turnNum++
			inTurn = true
			userText = text
		case "assistant":
			if !inTurn {
				continue
			}
			text := wireContentText(frame.Message.Content)
			if text == "" {
				continue
			}
			if assistantText != "" {
				assistantText += "\n"
			}
			assistantText += text
		case "agent_note":
			if !inTurn || strings.TrimSpace(frame.Text) == "" {
				continue
			}
			notes = append(notes, frame.Text)
		}
	}
	flush()
	return out
}

// wireEventsFromLines parses journal/chatlog lines into chat-wire events
// for applyEventTape. Turns drop tool_use; events do not.
func wireEventsFromLines(lines []string) []map[string]any {
	out := make([]map[string]any, 0, len(lines))
	for _, ln := range lines {
		var ev map[string]any
		if err := json.Unmarshal([]byte(ln), &ev); err != nil {
			continue
		}
		typ, _ := ev["type"].(string)
		if typ == "" {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// wireContentText extracts display text from a chat wire message.content,
// which is either a plain string (user echo) or an array of typed blocks
// (assistant). tool_use blocks are activity, not conversation — skipped.
func wireContentText(content json.RawMessage) string {
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(content, &s); err != nil {
			return ""
		}
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type != "text" || blk.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(blk.Text)
	}
	return b.String()
}

// sendToOverseerAsOwner delivers an owner turn to the overseer through the
// agent-addressed family with exactly the semantics of the /ws/chat wire:
// journal + broadcast the clean owner bubble, then deliver the turn carrying
// userTurnPrefix so the overseer can tell owner words from injected
// notifications (🎯T63). This is what makes send non-exclusive to /ws/chat.
func (s *Server) sendToOverseerAsOwner(text string) error {
	echo := chatUserEcho(text)
	s.NoteOwnerSend(text, echo)
	s.BroadcastChat(echo)
	if err := s.SendToOverseer(userTurnPrefix + text); err != nil {
		class, ownerMsg := agenterr.ClassifyAndFormat(err)
		if !class.IsFailure() {
			ownerMsg = err.Error()
		}
		slog.Error("chat: send to overseer failed",
			"err", err,
			"failure_class", class.String(),
			"transient", class.IsTransient(),
		)
		frame := map[string]string{
			"type":  "error",
			"error": "message not delivered: " + ownerMsg,
		}
		if class.IsFailure() {
			frame["failure_class"] = class.String()
		}
		payload, _ := json.Marshal(frame)
		s.BroadcastChat(string(payload))
		s.NoteOwnerResidual("delivery_failed")
		return err
	}
	return nil
}

// sendToOverseerAsAgent delivers an agent/system notification to the overseer
// (no owner marker, no owner bubble) — the same queue-on-busy path worker
// replies use, addressed by name.
func (s *Server) sendToOverseerAsAgent(text string) error {
	return s.SendToOverseer(text)
}

// DeliverToOverseerAs is the overseer arm of the fleet layer's single
// deliver-by-name path (🎯T309.3), wired from main into
// mcpserver.SetOverseerDeliver. Overseer delivery is implemented once, here:
// the HTTP send handler reaches it through sendToNamedAgentAs, and every
// fleet-layer caller (worker reply notify, worker-idle, daemon-restarted,
// MCP jevons_agent_send addressed to the overseer) reaches this same code
// through that seam. Neither side re-implements journalling, owner bubbles,
// or the notify queue.
//
// Origin uses the wire values of agentSendRequest.Origin so the two packages
// share a vocabulary without sharing a type (mcpserver does not import server).
func (s *Server) DeliverToOverseerAs(text, origin string) error {
	if origin == sendOriginAgent {
		return s.sendToOverseerAsAgent(text)
	}
	return s.sendToOverseerAsOwner(text)
}

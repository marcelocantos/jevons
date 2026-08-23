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
// hydrate, live subscribe, send — addressed uniformly by agent name, with
// the overseer as just another addressable agent:
//
//	history: writeInspectReplay (ReplayTailSealed named chat-wire)
//	live:    named chat-wire frames on /ws/chat
//	send:    sendToNamedAgentAs(name, text, origin)
//
// GET /api/history is the main chat's paging shim (load-earlier), not a
// second inspect dump. There is no GET /api/agents/{name}/transcript.

// overseerTranscriptMaxTurns caps journal.turns projection (not inspect
// hydrate — inspect uses historyReplayTurns via writeInspectReplay).
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
		s.observeProviderFailure(class, err.Error())
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
	s.observeProviderOK()
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

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"
)

// errNoTranscriptReader stands in for a provider read failure when no reader is
// attached at all, so the 🎯T367 journal merge takes the same path either way.
var errNoTranscriptReader = errors.New("transcript reader unavailable")

// 🎯T209: fleet agent inspect multiplexes over /ws/chat (same wire class as
// main owner chat). Client control frames:
//
//	{"type":"inspect_subscribe","name":"<agent>"}
//	{"type":"inspect_unsubscribe"}  // or name=""
//
// Server frames (never journaled into owner chatlog):
//
//	{"type":"agent_transcript","kind":"history","name":...,"turns":[...],...}
//	{"type":"agent_transcript","kind":"live","name":...,"event":{chat-wire-ish}}
//
// HTTP GET /api/agents/{name}/transcript remains for debug/export residual.

const (
	inspectKindHistory = "history" // leftover name; history is now a live-frame replay
	inspectKindLive    = "live"
	inspectKindReset   = "reset"
	// inspectHistoryTurns is the user-turn cap for an inspect history
	// frame — the same bound main chat uses on /ws/chat replay (🎯T533).
	inspectHistoryTurns = historyReplayTurns
	// inspectHistoryMaxEvents caps the raw events tape so applyEventTape
	// cannot freeze the tab on a 10k-event PO journal tail.
	inspectHistoryMaxEvents = 400
)

// setInspectSub binds ch to name (one inspect target per chat listener).
// Empty name clears. Safe from any goroutine; uses s.mu.
func (s *Server) setInspectSub(ch chan string, name string) {
	if s == nil || ch == nil {
		return
	}
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inspectByCh == nil {
		s.inspectByCh = make(map[chan string]string)
	}
	if s.inspectChans == nil {
		s.inspectChans = make(map[string]map[chan string]struct{})
	}
	if prev, ok := s.inspectByCh[ch]; ok && prev != "" {
		if m := s.inspectChans[prev]; m != nil {
			delete(m, ch)
			if len(m) == 0 {
				delete(s.inspectChans, prev)
			}
		}
		delete(s.inspectByCh, ch)
	}
	if name == "" {
		return
	}
	s.inspectByCh[ch] = name
	m := s.inspectChans[name]
	if m == nil {
		m = make(map[chan string]struct{})
		s.inspectChans[name] = m
	}
	m[ch] = struct{}{}
}

// clearInspectSub drops any inspect subscription for ch (disconnect).
func (s *Server) clearInspectSub(ch chan string) {
	s.setInspectSub(ch, "")
}

// inspectHasSubscribers reports whether any /ws/chat client is watching name.
func (s *Server) inspectHasSubscribers(name string) bool {
	if s == nil || name == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.inspectChans[name]
	return len(m) > 0
}

// fanInspectLive sends a non-journaled line to channels subscribed to name.
func (s *Server) fanInspectLive(name, line string) {
	if s == nil || name == "" || line == "" {
		return
	}
	s.mu.RLock()
	subs := s.inspectChans[name]
	var chans []chan string
	for ch := range subs {
		chans = append(chans, ch)
	}
	s.mu.RUnlock()
	for _, ch := range chans {
		select {
		case ch <- line:
		default:
			// Drop if slow client; history resync on terminal covers gaps.
		}
	}
}

// buildAgentTranscriptPayload loads sealed turns for name (same shape as HTTP).
// ok=false when agent is missing (caller may send error frame). Soft-empty
// (no session / read error / zero turns) still returns ok=true with empty turns.
func (s *Server) buildAgentTranscriptPayload(name string) (payload map[string]any, ok bool) {
	defer func() {
		if ok {
			clipInspectPayload(payload)
		}
	}()
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	// 🎯T309.2: the overseer is addressable like any other agent. This replaces
	// the 🎯T124 refusal ("overseer uses main chat"); its conversation is read
	// from the owner chat journal and projected into the same turn shape.
	if s.isOverseerAgent(name) {
		return s.buildOverseerTranscriptPayload(name), true
	}

	s.mu.RLock()
	reg := s.registry
	tr := s.transcriptReader
	s.mu.RUnlock()

	if reg == nil {
		return map[string]any{
			"type":  "agent_transcript",
			"kind":  inspectKindHistory,
			"name":  name,
			"turns": []any{},
			"empty": true,
			"error": "no agent registry",
		}, true
	}
	if tr == nil {
		// 🎯T367: a missing provider reader is no longer fatal to the pane when
		// jevons has its own record — fall through and serve the journal.
		if jt, _ := s.agentJournalTurns(name); len(jt) == 0 {
			return map[string]any{
				"type":  "agent_transcript",
				"kind":  inspectKindHistory,
				"name":  name,
				"turns": []any{},
				"empty": true,
				"error": "transcript reader unavailable",
			}, true
		}
	}

	var sessionID, purpose, workdir string
	found := false
	for _, d := range reg.List() {
		if d.Name == name {
			found = true
			sessionID = d.SessionID
			purpose = d.Purpose
			workdir = d.WorkDir
			break
		}
	}
	if !found {
		// 🎯T371: deregistration is not erasure. A stopped-and-reaped aside
		// (🎯T165) or an agent removed between paint and rehydrate still has a
		// 🎯T367 journal on disk holding the owner's own turns, and the caller's
		// only alternative here was an "agent not found" frame with zero turns —
		// which the client applied over the pane, deleting a conversation that
		// was durably recorded. Serve the journal when one exists; a genuinely
		// unknown name (no registry entry, no journal) still reports not-found.
		journalOnly, jerr := s.agentJournalTurns(name)
		if jerr != nil {
			slog.Warn("agent_chatlog_read_failed",
				"component", "agent_chatlog",
				"name", name,
				"err", jerr.Error(),
			)
			return nil, false
		}
		if len(journalOnly) == 0 {
			return nil, false
		}
		payload := map[string]any{
			"type":          "agent_transcript",
			"kind":          inspectKindHistory,
			"name":          name,
			"source":        conversationSourceAgentJournal,
			"turns":         renumberTurns(journalOnly),
			"journal_turns": len(journalOnly),
			"empty":         false,
			"unregistered":  true,
			"note":          "agent is no longer registered; serving jevons journal",
		}
		if evs, eerr := s.agentJournalsFor().events(name); eerr == nil && len(evs) > 0 {
			payload["events"] = evs
		}
		return payload, true
	}
	base := map[string]any{
		"type":       "agent_transcript",
		"kind":       inspectKindHistory,
		"name":       name,
		"purpose":    purpose,
		"workdir":    workdir,
		"session_id": sessionID,
		"source":     conversationSourceSession, // 🎯T309.2 family origin
	}

	// 🎯T367: the jevons-owned per-agent journal is read first and merged over
	// whatever the provider store yields, so sidebar rehydrate survives a
	// missing session id, an unreadable session file, and a daemon restart —
	// the same guarantee 🎯T30.1 gives main chat.
	journalTurns, jerr := s.agentJournalTurns(name)
	if jerr != nil {
		slog.Warn("agent_chatlog_read_failed",
			"component", "agent_chatlog",
			"name", name,
			"err", jerr.Error(),
		)
		journalTurns = nil
	}

	var turns []map[string]any
	var readErr error
	switch {
	case sessionID == "":
		base["note"] = "no session_id yet"
	case tr == nil:
		readErr = errNoTranscriptReader
	default:
		turns, readErr = tr.Read(sessionID)
		if readErr != nil {
			turns = nil
		}
	}
	if turns == nil {
		turns = []map[string]any{}
	}

	merged, journalUsed := mergeAgentTurns(turns, journalTurns)
	if journalUsed {
		base["source"] = conversationSourceAgentJournal
	}
	base["journal_turns"] = len(journalTurns)
	base["turns"] = merged
	base["empty"] = len(merged) == 0
	if evs, eerr := s.agentJournalsFor().events(name); eerr == nil && len(evs) > 0 {
		base["events"] = evs
	}

	// Soft-empty fingerprints stay attached to the reason the SESSION was
	// short, so `rg empty_reason=` still explains a thin pane (🎯T128.2) — but
	// a payload the journal filled is no longer reported as empty.
	switch {
	case sessionID == "":
		if len(merged) == 0 {
			s.logTranscriptEmpty(emptyReasonNoSession, name, "", "")
			base["empty_reason"] = emptyReasonNoSession
		}
	case readErr != nil:
		if len(merged) == 0 {
			s.logTranscriptEmpty(emptyReasonReadError, name, sessionID, readErr.Error())
			base["empty_reason"] = emptyReasonReadError
			// Only a pane the journal could not fill carries the provider's
			// read error; a rehydrated one is not decorated with ai-err chrome.
			base["error"] = readErr.Error()
		} else {
			base["session_error"] = readErr.Error()
		}
	case len(merged) == 0:
		s.logTranscriptEmpty(emptyReasonZeroTurns, name, sessionID, "")
		base["empty_reason"] = emptyReasonZeroTurns
	}
	return base, true
}

// marshalAgentTranscriptHistory returns the JSON history frame for name.
func (s *Server) marshalAgentTranscriptHistory(name string) (line string, ok bool) {
	payload, ok := s.buildAgentTranscriptPayload(name)
	if !ok {
		return "", false
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// writeInspectReplay hydrates inspect the same way /ws/chat hydrates main:
// ReplayTailSealed, one chat-wire frame at a time, applied via applyWireEvent.
type inspectWriter interface {
	Write(ctx context.Context, typ websocket.MessageType, data []byte) error
}

func (s *Server) writeInspectReplay(ctx context.Context, conn inspectWriter, name string) error {
	if s == nil || conn == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	reset, _ := json.Marshal(map[string]any{
		"type": "agent_transcript",
		"kind": inspectKindReset,
		"name": name,
	})
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := conn.Write(wctx, websocket.MessageText, reset)
	cancel()
	if err != nil {
		return err
	}
	writeLine := func(line string) error {
		if echoedOwnerTurnLine(line) {
			return nil
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil
		}
		frame, err := json.Marshal(map[string]any{
			"type":  "agent_transcript",
			"kind":  inspectKindLive,
			"name":  name,
			"event": ev,
		})
		if err != nil {
			return nil
		}
		wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, frame)
	}
	if s.isOverseerAgent(name) {
		s.mu.RLock()
		clog := s.chatLog
		s.mu.RUnlock()
		if clog == nil {
			return nil
		}
		_, _, err := clog.ReplayTailSealed(historyReplayTurns, writeLine)
		return err
	}
	j := s.agentJournalsFor()
	if j == nil {
		return nil
	}
	path := j.path(name)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	l := j.logFor(name)
	if l == nil {
		return nil
	}
	_, _, err = l.ReplayTailSealed(historyReplayTurns, writeLine)
	return err
}

// clipInspectPayload tails turns/events so one history frame cannot be
// the whole agent journal (🎯T533). Mutates payload in place.
func clipInspectPayload(payload map[string]any) {
	if payload == nil {
		return
	}
	if _, has := payload["turns"]; has {
		turns := mapsFromAny(payload["turns"])
		clipped, total, truncated := tailInspectTurns(turns, inspectHistoryTurns)
		payload["turns"] = clipped
		payload["history_turns"] = total
		payload["history_shown"] = countUserTurns(clipped)
		if truncated {
			payload["truncated"] = true
		}
	}
	if _, has := payload["events"]; has {
		evs := mapsFromAny(payload["events"])
		n := len(evs)
		tailed := tailInspectEvents(evs, inspectHistoryTurns, inspectHistoryMaxEvents)
		payload["events"] = tailed
		if len(tailed) < n {
			payload["truncated"] = true
		}
	}
}

// mapsFromAny coerces a JSON-decoded or in-process turns/events value
// into []map[string]any so clipInspectPayload does not miss a []any tape.
func mapsFromAny(v any) []map[string]any {
	switch t := v.(type) {
	case []map[string]any:
		return t
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func countUserTurns(turns []map[string]any) int {
	n := 0
	for _, t := range turns {
		if t == nil {
			continue
		}
		if role, _ := t["role"].(string); role == "user" {
			n++
		}
	}
	return n
}

func tailInspectTurns(turns []map[string]any, maxUser int) (out []map[string]any, totalUser int, truncated bool) {
	totalUser = countUserTurns(turns)
	if maxUser <= 0 || totalUser <= maxUser {
		return turns, totalUser, false
	}
	need := maxUser
	cut := 0
	for i := len(turns) - 1; i >= 0; i-- {
		t := turns[i]
		if t == nil {
			continue
		}
		if role, _ := t["role"].(string); role == "user" {
			need--
			if need == 0 {
				cut = i
				break
			}
		}
	}
	return turns[cut:], totalUser, true
}

func tailInspectEvents(evs []map[string]any, maxUser, maxEvents int) []map[string]any {
	if len(evs) == 0 {
		return evs
	}
	cut := 0
	if maxUser > 0 {
		need := maxUser
		for i := len(evs) - 1; i >= 0; i-- {
			e := evs[i]
			if e == nil {
				continue
			}
			t, _ := e["type"].(string)
			if t == "" {
				t, _ = e["role"].(string)
			}
			if t == "user" {
				need--
				if need == 0 {
					cut = i
					break
				}
			}
		}
	}
	if maxEvents > 0 && len(evs)-cut > maxEvents {
		cut = len(evs) - maxEvents
	}
	if cut <= 0 {
		return evs
	}
	return evs[cut:]
}

// inspectLiveEvent maps a fleet ACP event into an inspect progressive event
// (user/assistant text). Unlike chatWireLine, fleet user prompts stay as
// type=user (owner-chat converts unprefixed users to agent_note).
func inspectLiveEvent(ev claudia.Event) (event map[string]any, ok bool) {
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	switch ev.Type {
	case "user":
		if ev.Text == "" {
			if isClaudeShaped(ev.Raw) {
				var event map[string]any
				if err := json.Unmarshal(ev.Raw, &event); err != nil {
					return nil, false
				}
				return event, true
			}
			return nil, false
		}
		return map[string]any{
			"type":      "user",
			"timestamp": ts,
			"message": map[string]any{
				"role": "user",
				// 🎯T384: same typed-block shape as the assistant branch below,
				// so one consumer serves both roles on the live wire as well as
				// in the journal.
				"content": []map[string]any{
					{"type": "text", "text": ev.Text},
				},
			},
		}, true
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
			return map[string]any{
				"type":      "assistant",
				"timestamp": ts,
				"message":   msg,
			}, true
		}
		if ev.IsTerminalStop() {
			return map[string]any{
				"type":      "assistant",
				"timestamp": ts,
				"message": map[string]any{
					"role":        "assistant",
					"content":     []any{},
					"stop_reason": ev.StopReason,
				},
			}, true
		}
		return nil, false
	case "progress":
		// Same chat-wire tool_use frame the overseer apply already understands
		// (chatWireLine). Without this, fleet subscribe never delivers tools
		// and ⋯ n steps cannot appear on jevons-po.
		line, ok := chatWireLine(ev)
		if !ok {
			return nil, false
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, false
		}
		return event, true
	default:
		if isClaudeShaped(ev.Raw) {
			var event map[string]any
			if err := json.Unmarshal(ev.Raw, &event); err != nil {
				return nil, false
			}
			return event, true
		}
		return nil, false
	}
}

// DeliverInspectLive fans progressive inspect frames to /ws/chat subscribers
// for name (🎯T209). On terminal stop, also re-pushes sealed history so the
// pane resyncs with the durable transcript after stream coalescing.
// Safe from agent event hooks (any goroutine).
func (s *Server) DeliverInspectLive(name string, ev claudia.Event) {
	if s == nil || name == "" {
		return
	}
	// 🎯T367: journal first, unconditionally. Durability must not depend on a
	// sidebar happening to be open — this is the fleet mirror of BroadcastChat
	// appending every owner-chat line to the 🎯T30.1 journal.
	s.journalAgentEvent(name, ev)
	if !s.inspectHasSubscribers(name) {
		return
	}
	if event, ok := inspectLiveEvent(ev); ok {
		payload, err := json.Marshal(map[string]any{
			"type":  "agent_transcript",
			"kind":  inspectKindLive,
			"name":  name,
			"event": event,
		})
		if err == nil {
			s.fanInspectLive(name, string(payload))
		}
	}
	if ev.IsTerminalStop() {
		if line, ok := s.marshalAgentTranscriptHistory(name); ok {
			s.fanInspectLive(name, line)
		}
	}
}

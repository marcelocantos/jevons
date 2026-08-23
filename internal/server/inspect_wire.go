// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"
)

// errNoTranscriptReader is a sentinel tests and send-failure stubs use.
var errNoTranscriptReader = errors.New("transcript reader unavailable")

// 🎯T209: fleet agent inspect multiplexes over /ws/chat (same wire class as
// main owner chat). Client control frames:
//
//	{"type":"inspect_subscribe","name":"<agent>"}
//	{"type":"inspect_unsubscribe"}  // or name=""
//
// Hydrate is writeInspectReplay: conversation_reset then ReplayTailSealed
// named chat-wire frames. Live is DeliverInspectLive named frames. There is
// no history-blob dump and no GET /api/agents/{name}/transcript.

const (
	// inspectHistoryTurns is the user-turn cap for inspect journal replay —
	// the same bound main chat uses on /ws/chat (🎯T533).
	inspectHistoryTurns = historyReplayTurns
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
	s.muxFanTranscript(name, line)
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
		"type": "conversation_reset",
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
		frame := stampConversationName(line, name)
		wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, []byte(frame))
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
		event["name"] = name
		payload, err := json.Marshal(event)
		if err == nil {
			s.fanInspectLive(name, string(payload))
		}
	}
}

// stampConversationName puts the addressee on a chat-wire line so one
// /ws/chat multiplexes every agent. Missing/invalid JSON is left alone.
func stampConversationName(line, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.TrimSpace(line) == "" {
		return line
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return line
	}
	m["name"] = name
	b, err := json.Marshal(m)
	if err != nil {
		return line
	}
	return string(b)
}

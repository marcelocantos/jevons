// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"
)

// fakeAliveAgent is not a real claudia.Agent — handleChat requires
// CurrentProcess().Alive(). For the round-trip we drive events via
// DeliverOverseerEvent and only need the WS fan-out + client read path.
//
// handleChat closes with "claude not running" when proc is nil/dead, so
// this test exercises BroadcastChat listeners by attaching a listener
// channel directly (same path AttachOverseer → DeliverOverseerEvent uses)
// and a minimal WS server that mirrors handleChat's server→client side.

func TestChatWireRoundTripOverWebSocket(t *testing.T) {
	s := New("test", t.TempDir())

	// Mirror the live fan-out: DeliverOverseerEvent → BroadcastChat → WS write.
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, wsAcceptOptions())
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()

		ch := make(chan string, 64)
		s.mu.Lock()
		s.chatListeners = append(s.chatListeners, ch)
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			for i, l := range s.chatListeners {
				if l == ch {
					s.chatListeners = append(s.chatListeners[:i], s.chatListeners[i+1:]...)
					break
				}
			}
			s.mu.Unlock()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case line := <-ch:
				writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, []byte(line))
				cancel()
				if err != nil {
					return
				}
			}
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, strings.Replace(srv.URL, "http", "ws", 1)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// Give the accept goroutine a moment to register the listener.
	time.Sleep(20 * time.Millisecond)

	// Synthetic Grok turn.
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	s.BroadcastChat(chatUserEcho("ping"))
	s.DeliverOverseerEvent(claudia.Event{
		Type: "assistant",
		Text: "pong",
		Raw:  []byte(`{"sessionId":"x","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}`),
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		StopReason: "end_turn",
		Raw:        []byte(`{"stopReason":"end_turn"}`),
	})

	wantTypes := []string{"user", "assistant", "assistant"}
	var got []map[string]any
	for i := 0; i < len(wantTypes); i++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read %d: %v (got so far %v)", i, err, got)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("json %d: %v raw=%s", i, err, data)
		}
		got = append(got, m)
		if m["type"] != wantTypes[i] {
			t.Fatalf("msg %d type=%v want %s raw=%s", i, m["type"], wantTypes[i], data)
		}
	}

	// User bubble content.
	um, _ := got[0]["message"].(map[string]any)
	if um["content"] != "ping" {
		t.Fatalf("user content=%v", um["content"])
	}
	// Assistant text under content[].
	am, _ := got[1]["message"].(map[string]any)
	content, _ := am["content"].([]any)
	if len(content) == 0 {
		t.Fatal("assistant content empty")
	}
	block, _ := content[0].(map[string]any)
	if block["text"] != "pong" {
		t.Fatalf("assistant text=%v", block["text"])
	}
	// Terminal stop.
	tm, _ := got[2]["message"].(map[string]any)
	if tm["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason=%v", tm["stop_reason"])
	}

	// Simulate the browser pure rule on the received frames.
	working := true
	for _, m := range got {
		if shouldClearWorkingGo(m) {
			working = false
		}
	}
	if working {
		t.Fatal("working still true after full turn — UI would stay stuck")
	}
}

// shouldClearWorkingGo mirrors web/scripts/chat_events.js shouldClearWorking
// so the round-trip test fails if wire shape drifts from the frontend rule.
// Mid-stream text must NOT clear — only terminal stop_reason / system.
func shouldClearWorkingGo(m map[string]any) bool {
	typ, _ := m["type"].(string)
	if typ == "system" {
		return true
	}
	if typ != "assistant" {
		return false
	}
	msg, _ := m["message"].(map[string]any)
	if msg == nil {
		return false
	}
	if _, ok := msg["content"].([]any); !ok {
		return false
	}
	stop, _ := msg["stop_reason"].(string)
	if stop == "" {
		stop, _ = msg["stopReason"].(string)
	}
	return stop == "end_turn" || stop == "stop_sequence" || stop == "max_tokens"
}

// TestMultiChunkStreamWire is the server-side half of the token-per-bubble
// regression: many ACP text chunks must become many Claude-shaped wire
// lines that do not clear working until the terminal end_turn.
func TestMultiChunkStreamWire(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 64)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.mu.Unlock()

	tokens := []string{"Hello", ".", "What", "do", "you", "need", "?"}
	for _, tok := range tokens {
		s.DeliverOverseerEvent(claudia.Event{
			Type: "assistant",
			Text: tok,
			Raw:  []byte(`{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"` + tok + `"}}}`),
		})
	}
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		StopReason: "end_turn",
		Raw:        []byte(`{"stopReason":"end_turn"}`),
	})

	var lines []string
	deadline := time.After(2 * time.Second)
	for len(lines) < len(tokens)+1 {
		select {
		case l := <-ch:
			lines = append(lines, l)
		case <-deadline:
			t.Fatalf("timeout; got %d lines want %d", len(lines), len(tokens)+1)
		}
	}

	// Simulate pure frontend coalesce rules on the wire.
	working := true
	var bubbles []string
	open := -1
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if m["type"] != "assistant" {
			t.Fatalf("line %d type=%v", i, m["type"])
		}
		msg, _ := m["message"].(map[string]any)
		content, _ := msg["content"].([]any)
		for _, c := range content {
			cm, _ := c.(map[string]any)
			if cm["type"] == "text" {
				text, _ := cm["text"].(string)
				if text == "" {
					continue
				}
				// Mid-stream must not clear.
				if shouldClearWorkingGo(m) {
					t.Fatalf("chunk %d cleared working mid-stream: %s", i, line)
				}
				if open >= 0 {
					bubbles[open] += text
				} else {
					bubbles = append(bubbles, text)
					open = len(bubbles) - 1
				}
			}
		}
		if shouldClearWorkingGo(m) {
			working = false
			open = -1
		}
	}

	if working {
		t.Fatal("working still true after end_turn")
	}
	if len(bubbles) != 1 {
		t.Fatalf("coalesce produced %d bubbles %q; want 1 full sentence", len(bubbles), bubbles)
	}
	want := ""
	for _, tok := range tokens {
		want += tok
	}
	if bubbles[0] != want {
		t.Fatalf("bubble=%q want %q", bubbles[0], want)
	}

	s.mu.RLock()
	stillWaiting := s.waiting
	s.mu.RUnlock()
	if stillWaiting {
		t.Fatal("server waiting still true after terminal")
	}
}

// 🎯T54: when the overseer is down (e.g. a fresh install with no Grok
// CLI), a client connecting to /ws/chat must receive a legible error
// frame carrying the configured reason BEFORE the socket closes — not a
// bare, silent close a first-run stranger can't diagnose.
func TestChatOverseerDownSendsLegibleErrorFrame(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerDownReason("the Grok CLI is not installed — install it and restart")
	// No process attached and no chat log: handleChat hits the
	// overseer-down path directly.

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/chat", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("expected an error frame before close, got read error: %v", err)
	}
	var m struct {
		Type  string `json:"type"`
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &m) != nil || m.Type != "error" {
		t.Fatalf("first frame = %q, want an error frame", string(data))
	}
	if !strings.Contains(m.Error, "Grok CLI is not installed") {
		t.Fatalf("error frame %q does not carry the configured reason", m.Error)
	}
}

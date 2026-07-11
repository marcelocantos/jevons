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
	s := New(nil, "test")

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
	content, ok := msg["content"].([]any)
	if !ok {
		return false
	}
	hasText := false
	for _, c := range content {
		cm, _ := c.(map[string]any)
		if cm["type"] == "text" {
			if t, _ := cm["text"].(string); t != "" {
				hasText = true
			}
		}
	}
	stop, _ := msg["stop_reason"].(string)
	if stop == "" {
		stop, _ = msg["stopReason"].(string)
	}
	terminal := stop == "end_turn" || stop == "stop_sequence" || stop == "max_tokens"
	return terminal || (hasText && stop == "")
}

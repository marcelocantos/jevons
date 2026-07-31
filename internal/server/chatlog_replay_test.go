// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

// 🎯T30.1 replay oracle: the jevons-owned chat log — not the provider's
// private store — is what reconnecting clients replay, and it survives
// a dead overseer process. Completed turns always come back.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/marcelocantos/jevons/internal/chatlog"
)

// TestChatReplaysFromJevonsLogWithDeadOverseer simulates the restart
// scenario: turns were logged, the overseer process is gone, a client
// reconnects — history must still replay in full before the connection
// closes for lack of a live process.
func TestChatReplaysFromJevonsLogWithDeadOverseer(t *testing.T) {
	dir := t.TempDir()
	l, err := chatlog.Open(filepath.Join(dir, "chatlog", "jevons.jsonl"))
	if err != nil {
		t.Fatalf("chatlog.Open: %v", err)
	}
	defer l.Close()

	s := New("test", dir)
	s.SetChatLog(l)

	// Turns arrive through the normal broadcast path (which appends).
	lines := []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"ship the fix"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"Shipped."}]}}`,
	}
	for _, ln := range lines {
		s.BroadcastChat(ln)
	}

	// No overseer process attached — the "provider is dead" case.
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

	var got []string
	for range lines {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read after %d lines: %v (history lost)", len(got), err)
		}
		got = append(got, string(data))
	}
	if len(got) != 2 {
		t.Fatalf("replayed %d frames, want 2: %v", len(got), got)
	}
	// SealLines may re-marshal key order; assert semantic content.
	if !strings.Contains(got[0], `"type":"user"`) && !strings.Contains(got[0], `"type": "user"`) {
		// compact JSON from BroadcastChat uses no spaces
		if !strings.Contains(got[0], "user") {
			t.Fatalf("line0 not user: %s", got[0])
		}
	}
	if !strings.Contains(got[0], "ship the fix") {
		t.Fatalf("line0 missing user text: %s", got[0])
	}
	if !strings.Contains(got[1], "Shipped.") {
		t.Fatalf("line1 missing assistant text: %s", got[1])
	}
	if !strings.Contains(got[1], "end_turn") {
		t.Fatalf("line1 missing terminal stop: %s", got[1])
	}
}

// TestChatLogSurvivesServerRestart: a second Server over the same log
// path (daemon restart) replays what the first one recorded.
func TestChatLogSurvivesServerRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog", "jevons.jsonl")

	l1, err := chatlog.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s1 := New("test", dir)
	s1.SetChatLog(l1)
	s1.BroadcastChat(`{"type":"user","text":"before restart"}`)
	if err := l1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	l2, err := chatlog.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer l2.Close()
	var got []string
	if err := l2.Replay(func(line string) error { got = append(got, line); return nil }); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "before restart") {
		t.Fatalf("restart lost the turn: %v", got)
	}
}

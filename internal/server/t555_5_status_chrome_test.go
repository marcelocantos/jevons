// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/chatlog"
)

func TestEphemeralStatusLine(t *testing.T) {
	t.Parallel()
	if !isEphemeralChatStatusLine(`{"type":"status","text":"overseer is back"}`) {
		t.Fatal("recovery status must be ephemeral")
	}
	if !isEphemeralChatStatusLine(`{"text":"overseer is back","type":"status"}`) {
		t.Fatal("key order must not matter")
	}
	if isEphemeralChatStatusLine(`{"type":"assistant","message":{"content":[{"type":"text","text":"status"}]}}`) {
		t.Fatal("assistant prose mentioning status is a turn")
	}
	if isEphemeralChatStatusLine(`{"type":"user","message":{"content":[{"type":"text","text":"hi"}]}}`) {
		t.Fatal("user turn is not chrome")
	}
	if isEphemeralChatStatusLine(`not json`) {
		t.Fatal("malformed line is not chrome")
	}
}

func TestBroadcastCockpitReadyDoesNotJournalStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog", "jevons.jsonl")
	l, err := chatlog.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()

	s := New("test", dir)
	s.SetChatLog(l)

	live := make(chan string, 4)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, live)
	s.mu.Unlock()

	s.BroadcastChat(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`)
	s.broadcastCockpitReady("overseer is back")
	s.BroadcastChat(`{"type":"status","text":"overseer is back"}`)

	select {
	case line := <-live:
		if !strings.Contains(line, `"type":"user"`) {
			t.Fatalf("first live frame = %s, want user", line)
		}
	default:
		t.Fatal("user turn was not live-fanned")
	}
	select {
	case line := <-live:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("ready frame: %v (%s)", err, line)
		}
		if m["type"] != "status" || m["text"] != "overseer is back" {
			t.Fatalf("ready live frame = %s", line)
		}
	default:
		t.Fatal("recovery chrome must still live-fan so vanilla can clear degraded")
	}

	var got []string
	if err := l.Replay(func(line string) error {
		got = append(got, line)
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0], `"type":"user"`) {
		t.Fatalf("journal=%v; status chrome must not be a turn", got)
	}
	for _, line := range got {
		if isEphemeralChatStatusLine(line) {
			t.Fatalf("journaled status chrome: %s", line)
		}
	}
}

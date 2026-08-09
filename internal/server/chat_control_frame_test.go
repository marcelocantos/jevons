// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/chatlog"
)

// journalLines returns the durable owner chat log as non-empty lines.
func journalLines(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read journal: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// serverWithJournal builds a Server backed by a real durable chat log.
func serverWithJournal(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "jevons.jsonl")
	log, err := chatlog.Open(path)
	if err != nil {
		t.Fatalf("open chatlog: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })
	s := New("test", dir)
	s.SetChatLog(log)
	return s, path
}

// 🎯T362: the owner saw literal {"type":"ux_state","composer_blocked":false}
// as a chat bubble. A control frame reaching the owner-turn path is echoed AND
// appended to the durable journal, so every later reconnect replays it as a
// turn. This pins the server half: consumed as a frame, never journaled.
func TestUXStateFrameIsConsumedAndNeverJournaled(t *testing.T) {
	s, path := serverWithJournal(t)
	ch := make(chan string, 4)

	frames := []string{
		`{"type":"ux_state","composer_blocked":false}`,
		`{"type":"ux_state","composer_blocked":true,"reason":"overseer_down"}`,
	}
	for _, frame := range frames {
		if !s.handleChatControlFrame(context.Background(), nil, ch, frame) {
			t.Fatalf("frame not consumed as control: %s", frame)
		}
	}
	if got := journalLines(t, path); len(got) != 0 {
		t.Fatalf("ux_state frames journaled as owner turns: %q", got)
	}

	// The oracle is not vacuous: a real owner turn on the same journal does
	// land, and lands as a user message the client would paint.
	s.BroadcastChat(chatUserEcho("ship the leader research"))
	got := journalLines(t, path)
	if len(got) != 1 || !strings.Contains(got[0], "ship the leader research") {
		t.Fatalf("owner turn missing from journal: %q", got)
	}
	if strings.Contains(got[0], "ux_state") {
		t.Fatalf("journal contains a leaked frame: %q", got[0])
	}
}

// The frame is not merely dropped — it is the client's own report that the
// owner cannot submit, which is the observation the UX-degrade level needs
// (🎯T361 half of the same wire).
func TestUXStateFrameObservesComposerLevel(t *testing.T) {
	s, _ := serverWithJournal(t)
	ch := make(chan string, 4)

	blocked := `{"type":"ux_state","composer_blocked":true,"reason":"overseer_down"}`
	if !s.handleChatControlFrame(context.Background(), nil, ch, blocked) {
		t.Fatal("blocked frame not consumed")
	}
	s.ownerMu.Lock()
	h := s.ownerHealthLocked()
	gotBlocked, gotReason := h.composerBlocked, h.composerReason
	s.ownerMu.Unlock()
	if !gotBlocked || gotReason != "overseer_down" {
		t.Fatalf("composer level not observed: blocked=%v reason=%q", gotBlocked, gotReason)
	}

	if !s.handleChatControlFrame(context.Background(), nil, ch, `{"type":"ux_state","composer_blocked":false}`) {
		t.Fatal("recovery frame not consumed")
	}
	s.ownerMu.Lock()
	h = s.ownerHealthLocked()
	gotBlocked, gotReason = h.composerBlocked, h.composerReason
	s.ownerMu.Unlock()
	if gotBlocked || gotReason != "" {
		t.Fatalf("recovery not observed: blocked=%v reason=%q", gotBlocked, gotReason)
	}
}

// Owner prose is never swallowed by the frame gate — a vanished turn is a
// worse failure than a leaked one. Only a typed protocol frame is consumed.
func TestNonControlMessagesStayOwnerTurns(t *testing.T) {
	s, _ := serverWithJournal(t)
	ch := make(chan string, 4)

	prose := []string{
		"Fix the ux_state leak please.",
		`Look at {"type":"ux_state","composer_blocked":false} in my chat and kill it.`,
		`{"composer_blocked":false}`,
		`{"type":""}`,
		`{"type":"ux_state"`,
		`["type","ux_state"]`,
	}
	for _, msg := range prose {
		if s.handleChatControlFrame(context.Background(), nil, ch, msg) {
			t.Errorf("owner message consumed as a control frame: %q", msg)
		}
	}
}

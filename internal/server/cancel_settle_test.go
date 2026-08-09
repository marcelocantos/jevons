// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"testing"
)

// TestSettleCancelBroadcastsCancelSettled: after an interrupt, the server
// itself must end the turn and say so. A Claude Session is a TUI that
// stops without emitting any terminal event, so a client waiting for the
// provider would spin forever (🎯T282). Clients read state=cancel_settled
// as "not working" (web/scripts/chat_events.js workingLevelFromSample).
func TestSettleCancelBroadcastsCancelSettled(t *testing.T) {
	s := New("test", t.TempDir())

	frames := make(chan string, 8)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, frames)
	// Mid-turn state the cancel has to clear.
	s.waiting = true
	s.turnBuf = "1\n2\n3\n"
	s.overseerStreamID = "stream-1"
	s.overseerStreamAcc = "1\n2\n3\n"
	s.mu.Unlock()

	s.settleCancel()

	select {
	case line := <-frames:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("settle frame is not JSON: %v (%s)", err, line)
		}
		if m["type"] != "status" || m["state"] != "cancel_settled" {
			t.Fatalf("settle frame = %s, want type=status state=cancel_settled", line)
		}
	default:
		t.Fatal("no settle frame broadcast to chat clients after interrupt")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiting {
		t.Error("server still marked waiting after cancel")
	}
	if s.turnBuf != "" {
		t.Errorf("turn buffer %q kept after cancel", s.turnBuf)
	}
	if s.overseerStreamID != "" || s.overseerStreamAcc != "" {
		t.Errorf("open stream state kept after cancel: id=%q acc=%q",
			s.overseerStreamID, s.overseerStreamAcc)
	}
}

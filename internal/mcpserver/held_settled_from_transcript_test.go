// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/sendq"
)

const heldSeat = "cl-po-held"

// plantHeld queues text and drives it to the Uncertain state a closed confirm
// window leaves behind.
func plantHeld(t *testing.T, s *Server, text string) sendq.Entry {
	t.Helper()
	if _, err := s.enqueueAgentSend(heldSeat, text); err != nil {
		t.Fatal(err)
	}
	e, claimed, err := s.sendQueue().ClaimFront(heldSeat)
	if err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}
	if err := s.sendQueue().Resolve(heldSeat, e, sendq.Unverified, "grew within 45s but never gained a user message carrying this"); err != nil {
		t.Fatal(err)
	}
	return e
}

func userRecord(t *testing.T, text string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": text},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

// On 2026-09-21 claudia-po held seven attempts, the oldest 28 hours old, with
// 244 messages queued around them. Every one named a report_id that was in
// claudia-po's transcript: Claude Code had kept each message in its input
// queue until the running turn ended, which was after the 45s confirm window
// closed. Only an operator could clear them.
func TestHeldAttemptIsSettledOnceTheTranscriptShowsIt(t *testing.T) {
	s, dir := t401Server(t)
	delivered := "[Agent cl-t93-codex-handshake-step responded] report_id=20260920T182410Z-84b332a2\nLedger committed. Still waiting on the suite; the monitors will fire."
	lost := "[Agent cl-t92-silence-bound-oracle responded] report_id=20260920T190328Z-e50e0e37\nThe bound held across a daemon bounce; oracle attached."
	plantHeld(t, s, delivered)
	plantHeld(t, s, lost)

	transcript := filepath.Join(dir, "receiver.jsonl")
	if err := os.WriteFile(transcript, []byte(userRecord(t, "an earlier, unrelated prompt")), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := s.settleHeldAgainst(s.sendQueue(), heldSeat, transcript); n != 0 {
		t.Fatalf("settled %d attempts against a transcript that carries neither", n)
	}

	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(userRecord(t, delivered)); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if n := s.settleHeldAgainst(s.sendQueue(), heldSeat, transcript); n != 1 {
		t.Fatalf("settled %d attempts, want the one the transcript now shows", n)
	}
	left, err := s.sendQueue().Snapshot(heldSeat)
	if err != nil {
		t.Fatal(err)
	}
	// The control: the attempt the transcript does not show is still held.
	// Settling is evidence, never a sweep-out.
	if len(left) != 1 || left[0].Text != lost || left[0].State != sendq.Uncertain {
		t.Fatalf("queue after settling: %+v", left)
	}

	// An unchanged transcript has nothing new to say and is not re-read.
	if err := os.Chmod(transcript, 0o000); err != nil {
		t.Fatal(err)
	}
	if n := s.settleHeldAgainst(s.sendQueue(), heldSeat, transcript); n != 0 {
		t.Fatalf("settled %d attempts from an unchanged transcript", n)
	}
}

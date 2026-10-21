// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// Dropping a different entry must not call the owned attempt pending, and
// must not kick a drain. The attempt's own result is still the exit.
// Show of the same queue must not promise that messages flow past the pin.
func TestT7665SiblingDropDoesNotClearAnOwnedAttempt(t *testing.T) {
	const (
		name   = "claudia-po"
		owned  = "owned attempt"
		held   = "uncertain sibling"
		behind = "behind the attempt"
	)
	s, sender, _ := t418Daemon(t, t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if _, _, err := s.sendQueue().Append(name, held, now); err != nil {
		t.Fatal(err)
	}
	uncertain, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok {
		t.Fatalf("claim held: %+v %v %v", uncertain, ok, err)
	}
	if err := s.sendQueue().Resolve(name, uncertain, sendq.Unverified, "fate unknown"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sendQueue().Append(name, owned, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok || attempt.Text != owned || attempt.AttemptID == "" {
		t.Fatalf("claim owned: %+v %v %v", attempt, ok, err)
	}
	if _, _, err := s.sendQueue().Append(name, behind, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}

	shown := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "show"}))
	for _, want := range []string{attempt.ID, attempt.AttemptID, "will not confirm, requeue, or drop"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("show omits %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "keep delivering") {
		t.Fatalf("show promises flow-past while an attempt is owned:\n%s", shown)
	}

	dropped := reconcile(t, s, map[string]any{
		"name": name, "action": "drop", "entry_id": uncertain.ID, "attempt_id": uncertain.AttemptID,
		"actor": "owner", "evidence": "the receiver has no copy",
	})
	if dropped.IsError {
		t.Fatalf("drop of the sibling failed: %s", resultText(t, dropped))
	}
	text := resultText(t, dropped)
	for _, want := range []string{attempt.ID, attempt.AttemptID, "will not confirm, requeue, or drop", "definite non-send"} {
		if !strings.Contains(text, want) {
			t.Fatalf("sibling drop omits %q:\n%s", want, text)
		}
	}
	for _, banned := range []string{"no longer blocked", "Kicking a drain", "remaining messages are pending", "keep delivering"} {
		if strings.Contains(text, banned) {
			t.Fatalf("sibling drop says %q:\n%s", banned, text)
		}
	}
	if len(sender.delivered()) != 0 {
		t.Fatalf("sibling drop delivered %q", sender.delivered())
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 2 || rows[0].ID != attempt.ID || rows[0].State != sendq.Attempting || rows[0].Text != owned || rows[1].Text != behind || rows[1].State != sendq.Pending {
		t.Fatalf("queue after sibling drop = %+v %v", rows, err)
	}
}

// An owned attempt whose file omits DeliveryState is still in flight.
// The tool must refuse confirm, requeue, and drop.
func TestT7665OwnedOmittedStateToolRefusesReconcile(t *testing.T) {
	const (
		name    = "claudia-po"
		payload = "owned leftover"
	)
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if _, _, err := s.sendQueue().Append(name, payload, now); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok || claimed.AttemptID == "" {
		t.Fatalf("claim: %+v %v %v", claimed, ok, err)
	}
	queueDir := filepath.Join(dir, "sendq")
	raw := `{"agent":"` + name + `","entries":[{"id":"` + claimed.ID + `","text":"` + payload + `","enqueued_at":"2026-10-19T12:00:00Z","attempt_id":"` + claimed.AttemptID + `"}]}`
	if err := os.WriteFile(filepath.Join(queueDir, name+".json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"confirmed", "requeue", "drop"} {
		res := reconcile(t, s, map[string]any{
			"name": name, "action": action, "entry_id": claimed.ID, "attempt_id": claimed.AttemptID,
			"actor": "owner", "evidence": "transcript has no copy",
		})
		text := resultText(t, res)
		if !res.IsError || !strings.Contains(text, "ACTIVE") {
			t.Fatalf("%s did not refuse the owned attempt: error=%v\n%s", action, res.IsError, text)
		}
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 1 || rows[0].Text != payload || rows[0].State != sendq.Pending || rows[0].AttemptID != claimed.AttemptID || sendq.EntryPhase(rows[0], claimed.AttemptID) != sendq.PhaseInFlight {
		t.Fatalf("tool changed the owned attempt: %+v %v", rows, err)
	}
}

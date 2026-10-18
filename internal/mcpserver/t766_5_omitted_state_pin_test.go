// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// A file that omits DeliveryState beside an attempt id is a held attempt.
// The operator notice used to call that shape pending and say a start would
// deliver it. Claim does not take it. The exit is jevons_sendq_reconcile.
func TestT7665OmittedStatePinNamesReconcile(t *testing.T) {
	const (
		name    = "claudia-po"
		payload = "kept leftover"
		entryID = "e-omit"
		attempt = "att-omit"
	)
	dir := t.TempDir()
	s, sender, _ := t418Daemon(t, dir)
	queueDir := filepath.Join(dir, "sendq")
	if err := os.MkdirAll(queueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"agent":"` + name + `","entries":[{"id":"` + entryID + `","text":"` + payload + `","enqueued_at":"2026-10-19T12:00:00Z","attempt_id":"` + attempt + `"}]}`
	if err := os.WriteFile(filepath.Join(queueDir, name+".json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	pin, pinned := s.sendqPinFor(name)
	if !pinned || pin.EntryID != entryID || pin.AttemptID != attempt || pin.State != sendq.Pending {
		t.Fatalf("pin = %+v pinned=%v", pin, pinned)
	}
	line := FormatSendqPinLine(name, pin)
	for _, want := range []string{entryID, attempt, "jevons_sendq_reconcile", "will not retry", "unresolved held attempt"} {
		if !strings.Contains(line, want) {
			t.Fatalf("pin line omits %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "A start would deliver") {
		t.Fatalf("pin line offers a start for a held attempt:\n%s", line)
	}

	shown := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "show"}))
	for _, want := range []string{entryID, attempt, "held attempt", "jevons_sendq_reconcile", "will not retry"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("show omits %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "A start would deliver") {
		t.Fatalf("show offers a start for a held attempt:\n%s", shown)
	}

	drained := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "drain"}))
	for _, want := range []string{entryID, attempt, "jevons_sendq_reconcile"} {
		if !strings.Contains(drained, want) {
			t.Fatalf("drain omits %q:\n%s", want, drained)
		}
	}
	if len(sender.delivered()) != 0 {
		t.Fatalf("drain delivered the held leftover: %q", sender.delivered())
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 1 || rows[0].Text != payload || rows[0].AttemptID != attempt || sendq.EntryPhase(rows[0], "") != sendq.PhaseHeldAttempt {
		t.Fatalf("drain changed the leftover: %+v %v", rows, err)
	}

	requeued := reconcile(t, s, map[string]any{
		"name": name, "action": "requeue", "entry_id": entryID, "attempt_id": attempt,
		"actor": "owner", "evidence": "transcript has no copy",
	})
	if requeued.IsError {
		t.Fatalf("requeue failed: %s", resultText(t, requeued))
	}
	rows, err = s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 1 || rows[0].ID != entryID || rows[0].Text != payload || rows[0].AttemptID != "" || rows[0].State != sendq.Pending || sendq.EntryPhase(rows[0], "") != sendq.PhaseRequeued {
		t.Fatalf("after requeue = %+v %v", rows, err)
	}

	again := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "drain"}))
	got := sender.delivered()
	if len(got) != 1 || got[0] != payload {
		t.Fatalf("requeued leftover was not offered: delivered=%q reply=\n%s", got, again)
	}
	rows, err = s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 1 || rows[0].Text != payload {
		t.Fatalf("offer destroyed the payload: %+v %v", rows, err)
	}
}

func TestT7665AdmittedPinStillSaysStart(t *testing.T) {
	line := FormatSendqPinLine("claudia-po", SendqPin{EntryID: "e1", Reason: "provider cannot accept it"})
	if !strings.Contains(line, "A start would deliver") {
		t.Fatalf("admitted pin lost the start exit:\n%s", line)
	}
	if strings.Contains(line, "will not retry") {
		t.Fatalf("admitted pin says the attempt will not retry:\n%s", line)
	}
}

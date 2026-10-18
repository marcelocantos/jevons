// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// An attempt this daemon owns is not pending and is not an orphan.
// Show and drain name that attempt's own result as the exit. Reconcile
// refuses it. A definite non-send returns the same text to pending, and
// the next drain offers that text.
func TestT7665InFlightNamesItsOwnResultAsTheExit(t *testing.T) {
	const (
		name    = "claudia-po"
		payload = "owned attempt"
		behind  = "behind the attempt"
	)
	s, sender, _ := t418Daemon(t, t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if _, _, err := s.sendQueue().Append(name, payload, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sendQueue().Append(name, behind, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok || attempt.AttemptID == "" || attempt.Text != payload {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}

	shown := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "show"}))
	for _, want := range []string{attempt.ID, attempt.AttemptID, "in flight", "will not confirm, requeue, or drop", "definite non-send"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("show omits %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "Nothing is unresolved") || strings.Contains(shown, "A start would deliver") {
		t.Fatalf("show offers drain or a start for an owned attempt:\n%s", shown)
	}

	drained := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "drain"}))
	for _, want := range []string{attempt.ID, attempt.AttemptID, "in flight", "will not confirm, requeue, or drop"} {
		if !strings.Contains(drained, want) {
			t.Fatalf("drain omits %q:\n%s", want, drained)
		}
	}
	if strings.Contains(drained, "offered again at its next boundary") || strings.Contains(drained, "one per turn boundary") {
		t.Fatalf("drain says the blocked queue will move:\n%s", drained)
	}
	if len(sender.delivered()) != 0 {
		t.Fatalf("drain delivered past the owned attempt: %q", sender.delivered())
	}

	refused := reconcile(t, s, map[string]any{
		"name": name, "action": "requeue", "entry_id": attempt.ID, "attempt_id": attempt.AttemptID,
		"actor": "owner", "evidence": "transcript has no copy",
	})
	if !refused.IsError || !strings.Contains(resultText(t, refused), "ACTIVE") {
		t.Fatalf("reconcile accepted an owned attempt: %s", resultText(t, refused))
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 2 || rows[0].ID != attempt.ID || rows[0].Text != payload || rows[0].State != sendq.Attempting || rows[1].Text != behind || rows[1].State != sendq.Pending {
		t.Fatalf("refused reconcile changed the queue: %+v %v", rows, err)
	}

	if err := s.sendQueue().Resolve(name, attempt, sendq.DefinitelyNotSent, "cannot accept"); err != nil {
		t.Fatal(err)
	}
	rows, err = s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 2 || rows[0].ID != attempt.ID || rows[0].Text != payload || rows[0].AttemptID != "" || sendq.EntryPhase(rows[0], "") != sendq.PhaseAdmitted || rows[1].Text != behind {
		t.Fatalf("not-sent did not return the same entry: %+v %v", rows, err)
	}

	again := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "drain"}))
	got := sender.delivered()
	if len(got) != 1 || got[0] != payload {
		t.Fatalf("returned entry was not offered: delivered=%q reply=\n%s", got, again)
	}
	rows, err = s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 2 || rows[0].Text != payload || rows[1].Text != behind {
		t.Fatalf("offer destroyed a payload: %+v %v", rows, err)
	}
}

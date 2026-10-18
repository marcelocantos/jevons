// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// A drain with no live process must not tell the operator to start the seat
// while this daemon still owns the attempt. Starting the seat does not record
// that attempt's result, and the queue does not move until it does.
func TestT7665NoLiveOwnedAttemptDoesNotPromiseAStart(t *testing.T) {
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
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return nil, false, nil })

	drained := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "drain"}))
	for _, want := range []string{attempt.ID, attempt.AttemptID, "no live process", "will not confirm, requeue, or drop"} {
		if !strings.Contains(drained, want) {
			t.Fatalf("drain omits %q:\n%s", want, drained)
		}
	}
	for _, banned := range []string{"first turn boundary", "pending message", "jevons_agent_start", "drain past it"} {
		if strings.Contains(drained, banned) {
			t.Fatalf("drain promises a start while the attempt is owned (%q):\n%s", banned, drained)
		}
	}
	if len(sender.delivered()) != 0 {
		t.Fatalf("drain delivered past the owned attempt: %q", sender.delivered())
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 2 || rows[0].ID != attempt.ID || rows[0].Text != payload || rows[0].State != sendq.Attempting || rows[1].Text != behind || rows[1].State != sendq.Pending {
		t.Fatalf("drain changed the queue: %+v %v", rows, err)
	}
}

// A live drain that also has a held sibling must not say messages flow past
// that sibling while an owned attempt blocks the seat.
func TestT7665OwnedAttemptDrainDoesNotFlowPastAPin(t *testing.T) {
	const (
		name   = "claudia-po"
		held   = "uncertain sibling"
		owned  = "owned attempt"
		behind = "behind the attempt"
	)
	s, sender, _ := t418Daemon(t, t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if _, _, err := s.sendQueue().Append(name, held, now); err != nil {
		t.Fatal(err)
	}
	sibling, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok || sibling.Text != held {
		t.Fatalf("claim sibling: %+v %v %v", sibling, ok, err)
	}
	if err := s.sendQueue().Resolve(name, sibling, sendq.Unverified, "fate unknown"); err != nil {
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

	drained := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "drain"}))
	for _, want := range []string{sibling.AttemptID, attempt.AttemptID, "blocks the seat", "will not confirm, requeue, or drop"} {
		if !strings.Contains(drained, want) {
			t.Fatalf("drain omits %q:\n%s", want, drained)
		}
	}
	for _, banned := range []string{"drain past it", "keep delivering", "offered again at its next boundary"} {
		if strings.Contains(drained, banned) {
			t.Fatalf("drain promises flow-past while the attempt is owned (%q):\n%s", banned, drained)
		}
	}
	if len(sender.delivered()) != 0 {
		t.Fatalf("drain delivered past the owned attempt: %q", sender.delivered())
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 3 || rows[0].Text != held || rows[0].State != sendq.Uncertain || rows[1].ID != attempt.ID || rows[1].State != sendq.Attempting || rows[2].Text != behind || rows[2].State != sendq.Pending {
		t.Fatalf("drain changed the queue: %+v %v", rows, err)
	}
}

// A queue whose only row is uncertain is not pending. Starting the seat does
// not take it, and a live drain does not offer it again at the next boundary.
func TestT7665HeldOnlyDrainDoesNotPromiseAStartOrARetry(t *testing.T) {
	const name, payload = "claudia-po", "fate unknown"
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	s, sender, _ := t418Daemon(t, t.TempDir())
	held := plantUncertain(t, s, name, payload, now)
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return nil, false, nil })
	drained := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "drain"}))
	for _, want := range []string{held.ID, held.AttemptID, "no live process", "jevons_sendq_reconcile"} {
		if !strings.Contains(drained, want) {
			t.Fatalf("no-live drain omits %q:\n%s", want, drained)
		}
	}
	for _, banned := range []string{"pending message", "jevons_agent_start", "first turn boundary", "drain past it"} {
		if strings.Contains(drained, banned) {
			t.Fatalf("no-live drain promises a start (%q):\n%s", banned, drained)
		}
	}
	if len(sender.delivered()) != 0 {
		t.Fatalf("no-live drain delivered the hold: %q", sender.delivered())
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 1 || rows[0].ID != held.ID || rows[0].Text != payload || rows[0].State != sendq.Uncertain {
		t.Fatalf("no-live drain changed the hold: %+v %v", rows, err)
	}

	live, liveSender, _ := t418Daemon(t, t.TempDir())
	liveHeld := plantUncertain(t, live, name, payload, now)
	drained = resultText(t, reconcile(t, live, map[string]any{"name": name, "action": "drain"}))
	for _, want := range []string{liveHeld.ID, liveHeld.AttemptID, "jevons_sendq_reconcile"} {
		if !strings.Contains(drained, want) {
			t.Fatalf("live drain omits %q:\n%s", want, drained)
		}
	}
	for _, banned := range []string{"pending message", "jevons_agent_start", "offered again at its next boundary", "drain past it"} {
		if strings.Contains(drained, banned) {
			t.Fatalf("live drain offers a held entry again (%q):\n%s", banned, drained)
		}
	}
	if len(liveSender.delivered()) != 0 {
		t.Fatalf("live drain delivered the hold: %q", liveSender.delivered())
	}
	rows, err = live.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 1 || rows[0].ID != liveHeld.ID || rows[0].Text != payload || rows[0].State != sendq.Uncertain {
		t.Fatalf("live drain changed the hold: %+v %v", rows, err)
	}
}

// The no-live start sentence counts claimable rows. A held row in front is
// not one of them, and the start call stays, because the claimable row drains
// once the seat has a process.
func TestT7665NoLivePendingCountSkipsTheHeldRow(t *testing.T) {
	const name = "claudia-po"
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	s, _, _ := t418Daemon(t, t.TempDir())
	held := plantUncertain(t, s, name, "uncertain sibling", now)
	if _, _, err := s.sendQueue().Append(name, "still waiting", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return nil, false, nil })

	drained := resultText(t, reconcile(t, s, map[string]any{"name": name, "action": "drain"}))
	for _, want := range []string{"1 pending message", "jevons_agent_start", held.ID, "drain past it"} {
		if !strings.Contains(drained, want) {
			t.Fatalf("drain omits %q:\n%s", want, drained)
		}
	}
	if strings.Contains(drained, "2 pending message") {
		t.Fatalf("drain counted the held row as pending:\n%s", drained)
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 2 || rows[0].State != sendq.Uncertain || rows[1].Text != "still waiting" || rows[1].State != sendq.Pending {
		t.Fatalf("drain changed the queue: %+v %v", rows, err)
	}
}

func plantUncertain(t *testing.T, s *Server, name, text string, at time.Time) sendq.Entry {
	t.Helper()
	if _, _, err := s.sendQueue().Append(name, text, at); err != nil {
		t.Fatal(err)
	}
	e, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok || e.Text != text {
		t.Fatalf("claim: %+v %v %v", e, ok, err)
	}
	if err := s.sendQueue().Resolve(name, e, sendq.Unverified, "fate unknown"); err != nil {
		t.Fatal(err)
	}
	return e
}

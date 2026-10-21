// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"testing"
	"time"
)

// Confirmed and drop are the Reconcile exits that remove an entry. The
// payload is archived under that entry's id before the queue changes.
// Requeue keeps the entry and writes no archive. Deleting the archive call
// leaves the queue empty and fails the confirmed and drop reads. Archiving
// the requeue fails its read.
func TestT7665ReconcileArchivesConfirmedAndDropAndLeavesRequeue(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	confirmed := admitUncertain(t, q, "a", "confirmed reconcile copy", now)
	if _, err := q.Reconcile("a", confirmed.ID, confirmed.AttemptID, ReconcileConfirmed, "owner", "receiver has the user message", now); err != nil {
		t.Fatal(err)
	}
	dropped := admitUncertain(t, q, "a", "dropped reconcile copy", now.Add(time.Second))
	if _, err := q.Reconcile("a", dropped.ID, dropped.AttemptID, ReconcileDrop, "owner", "abandon this copy", now); err != nil {
		t.Fatal(err)
	}
	requeued := admitUncertain(t, q, "a", "requeued reconcile copy", now.Add(2*time.Second))
	if _, err := q.Reconcile("a", requeued.ID, requeued.AttemptID, ReconcileRequeue, "owner", "transcript has no copy", now); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Reconcile("a", requeued.ID, "", ReconcileConfirmed, "owner", "", now); err == nil {
		t.Fatal("unevidenced reconcile was accepted")
	}

	left, err := q.Snapshot("a")
	if err != nil {
		t.Fatal(err)
	}
	var sawRequeue bool
	for _, e := range left {
		switch e.ID {
		case confirmed.ID, dropped.ID:
			t.Fatalf("removed entry still queued: %+v", e)
		case requeued.ID:
			sawRequeue = e.Text == "requeued reconcile copy" && EntryPhase(e, "") == PhaseRequeued
		default:
			t.Fatalf("unexpected entry: %+v", e)
		}
	}
	if !sawRequeue || len(left) != 1 {
		t.Fatalf("queue = %+v", left)
	}
	for _, want := range []struct{ id, text string }{
		{confirmed.ID, "confirmed reconcile copy"},
		{dropped.ID, "dropped reconcile copy"},
	} {
		got, err := q.ReadSupersession(want.id)
		if err != nil || len(got.Removed) != 1 || got.Removed[0].ID != want.id || got.Removed[0].Text != want.text {
			t.Fatalf("archive %s = %+v %v", want.id, got, err)
		}
	}
	if _, err := q.ReadSupersession(requeued.ID); err == nil {
		t.Fatal("requeued entry was archived")
	}
}

// Consolidate removes every entry except the survivor. Each removed payload
// is archived under its own id. The survivor stays in the queue and has no
// archive, including when its text is replaced.
func TestT7665ConsolidateArchivesTheFoldedPayloads(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	first, _, err := q.Append("a", "first instruction", now)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := q.Append("a", "second instruction", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	third, _, err := q.Append("a", "third instruction", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Consolidate("a", third.ID, "authoritative instruction", "owner", "the first two are superseded", now); err != nil {
		t.Fatal(err)
	}
	left, err := q.Snapshot("a")
	if err != nil || len(left) != 1 || left[0].ID != third.ID || left[0].Text != "authoritative instruction" {
		t.Fatalf("survivor = %+v %v", left, err)
	}
	for _, want := range []struct{ id, text string }{
		{first.ID, "first instruction"},
		{second.ID, "second instruction"},
	} {
		got, err := q.ReadSupersession(want.id)
		if err != nil || len(got.Removed) != 1 || got.Removed[0].ID != want.id || got.Removed[0].Text != want.text {
			t.Fatalf("archive %s = %+v %v", want.id, got, err)
		}
	}
	if _, err := q.ReadSupersession(third.ID); err == nil {
		t.Fatal("survivor was archived")
	}
}

func admitUncertain(t *testing.T, q *Store, agent, text string, at time.Time) Entry {
	t.Helper()
	admitted, _, err := q.Append(agent, text, at)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront(agent)
	if err != nil || !ok || attempt.ID != admitted.ID {
		t.Fatalf("claim %q: %+v %v %v", text, attempt, ok, err)
	}
	if err := q.Resolve(agent, attempt, Unverified, "no receiver evidence"); err != nil {
		t.Fatal(err)
	}
	left, err := q.Snapshot(agent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		if e.ID == admitted.ID {
			return e
		}
	}
	t.Fatalf("uncertain %q missing from %+v", text, left)
	return Entry{}
}

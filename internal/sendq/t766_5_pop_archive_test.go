// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"testing"
	"time"
)

// An in-flight attempt blocks PopFront, and a requeue keeps its entry.
// Neither writes an archive. Deleting the in-flight refusal, or archiving
// inside Reconcile requeue, fails this test. A later pop of the requeued
// copy is a claimable removal, so that text is archived then.
func TestT7665PopLeavesInFlightAndRequeueUnarchived(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	inflight, _, err := q.Append("a", "in flight copy", now)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || attempt.ID != inflight.ID {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	if _, ok, err := q.PopFront("a", "operator", "must not take a live attempt", now); err == nil || ok {
		t.Fatalf("in-flight pop = ok %v err %v", ok, err)
	}
	left, err := q.Snapshot("a")
	if err != nil || len(left) != 1 || left[0].ID != inflight.ID || left[0].Text != "in flight copy" || EntryPhase(left[0], attempt.AttemptID) != PhaseInFlight {
		t.Fatalf("in-flight queue = %+v %v", left, err)
	}
	if _, err := q.ReadSupersession(inflight.ID); err == nil {
		t.Fatal("in-flight pop archived the payload")
	}

	requeued := admitUncertain(t, q, "b", "requeued copy", now)
	if _, err := q.Reconcile("b", requeued.ID, requeued.AttemptID, ReconcileRequeue, "owner", "transcript has no copy", now); err != nil {
		t.Fatal(err)
	}
	held, err := q.Snapshot("b")
	if err != nil || len(held) != 1 || held[0].ID != requeued.ID || held[0].Text != "requeued copy" || EntryPhase(held[0], "") != PhaseRequeued {
		t.Fatalf("requeue = %+v %v", held, err)
	}
	if _, err := q.ReadSupersession(requeued.ID); err == nil {
		t.Fatal("requeue archived the payload")
	}

	rec, ok, err := q.PopFront("b", "operator", "owner took the requeued copy", now)
	if err != nil || !ok || len(rec.Removed) != 1 || rec.Removed[0].Text != "requeued copy" {
		t.Fatalf("requeued pop = %+v %v %v", rec, ok, err)
	}
	archived, err := q.ReadSupersession(requeued.ID)
	if err != nil || len(archived.Removed) != 1 || archived.Removed[0].ID != requeued.ID || archived.Removed[0].Text != "requeued copy" {
		t.Fatalf("requeued pop archive = %+v %v", archived, err)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"testing"
	"time"
)

// Pending counts admitted, digest, and requeued. In-flight, an orphaned
// attempt, and an uncertain hold stay in Depth and are not pending.
// Counting Depth as pending fails the in-flight, uncertain, and held cases.
func TestT7665PendingCountsClaimablePhasesOnly(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	if _, _, err := q.Append("admitted", "admitted copy", now); err != nil {
		t.Fatal(err)
	}
	if b, err := q.Backlog("admitted"); err != nil || b.Pending != 1 || b.Depth != 1 {
		t.Fatalf("admitted = %+v %v", b, err)
	}

	inflight, _, err := q.Append("flight", "in flight copy", now)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("flight")
	if err != nil || !ok || attempt.ID != inflight.ID {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	if _, _, err := q.Append("flight", "behind the attempt", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if b, err := q.Backlog("flight"); err != nil || b.Pending != 1 || b.Depth != 2 || b.InFlight != 1 {
		t.Fatalf("in flight = %+v %v", b, err)
	}

	uncertain := admitUncertain(t, q, "uncertain", "uncertain copy", now)
	if b, err := q.Backlog("uncertain"); err != nil || b.Pending != 0 || b.Depth != 1 || b.Uncertain != 1 {
		t.Fatalf("uncertain = %+v %v", b, err)
	}
	if _, err := q.Reconcile("uncertain", uncertain.ID, uncertain.AttemptID, ReconcileRequeue, "owner", "transcript has no copy", now); err != nil {
		t.Fatal(err)
	}
	requeued, err := q.Snapshot("uncertain")
	if err != nil || len(requeued) != 1 || EntryPhase(requeued[0], "") != PhaseRequeued {
		t.Fatalf("requeue = %+v %v", requeued, err)
	}
	if b, err := q.Backlog("uncertain"); err != nil || b.Pending != 1 || b.Depth != 1 {
		t.Fatalf("requeued pending = %+v %v", b, err)
	}

	for i := 0; i <= DigestThreshold; i++ {
		if _, _, err := q.Append("digest", "m", now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	digest, ok, err := q.ClaimDigest("digest")
	if err != nil || !ok || digest.Members == "" {
		t.Fatalf("digest = %+v %v %v", digest, ok, err)
	}
	if b, err := q.Backlog("digest"); err != nil || b.Pending != 0 || b.Depth != 1 || b.InFlight != 1 {
		t.Fatalf("digest in flight = %+v %v", b, err)
	}
	if err := q.Resolve("digest", digest, DefinitelyNotSent, "cannot accept"); err != nil {
		t.Fatal(err)
	}
	left, err := q.Snapshot("digest")
	if err != nil || len(left) != 1 || EntryPhase(left[0], "") != PhaseDigest {
		t.Fatalf("digest phase = %+v %v", left, err)
	}
	if b, err := q.Backlog("digest"); err != nil || b.Pending != 1 || b.Depth != 1 {
		t.Fatalf("digest pending = %+v %v", b, err)
	}

	dir := t.TempDir()
	owned := NewStore(dir)
	held, _, err := owned.Append("held", "orphaned attempt", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := owned.ClaimFront("held"); err != nil || !ok {
		t.Fatalf("held claim: %v %v", ok, err)
	}
	reopened := NewStore(dir)
	rows, err := reopened.Snapshot("held")
	if err != nil || len(rows) != 1 || rows[0].ID != held.ID || EntryPhase(rows[0], "") != PhaseHeldAttempt {
		t.Fatalf("held phase = %+v %v", rows, err)
	}
	if b, err := reopened.Backlog("held"); err != nil || b.Pending != 0 || b.Depth != 1 || b.BlockReason != "held attempt" {
		t.Fatalf("held pending = %+v %v", b, err)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"testing"
	"time"
)

// 🎯T766.5: an entry whose delivery nobody can establish must not poison the
// queue behind it. It is held — never offered again — while later entries
// keep delivering. A send in flight still serialises the seat.

func TestT766UncertainEntryIsHeldWhileLaterEntriesDeliver(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	first, _, err := q.Append("a", "ambiguous", now)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := q.Append("a", "behind it", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || attempt.ID != first.ID {
		t.Fatalf("claim first: %+v %v %v", attempt, ok, err)
	}
	if err := q.Resolve("a", attempt, Unverified, "no session event within 45s"); err != nil {
		t.Fatal(err)
	}

	next, ok, err := q.ClaimFront("a")
	if err != nil || !ok || next.ID != second.ID {
		t.Fatalf("queue did not flow past the uncertain entry: %+v %v %v", next, ok, err)
	}
	if held, blocked, err := q.BlockedHead("a"); err != nil || !blocked || held.ID != first.ID {
		t.Fatalf("uncertain entry not reported as held while the queue flows: %+v %v %v", held, blocked, err)
	}
	if err := q.Resolve("a", next, Confirmed, "payload seen"); err != nil {
		t.Fatalf("resolve a non-head attempt by id: %v", err)
	}

	left, err := q.Snapshot("a")
	if err != nil || len(left) != 1 || left[0].ID != first.ID || left[0].State != Uncertain {
		t.Fatalf("uncertain entry not kept, in place, after the one behind it delivered: %+v %v", left, err)
	}
	// Nothing is claimable: the held entry may be reported, never claimed.
	if again, ok, err := q.ClaimFront("a"); err != nil || ok || again.State != Uncertain {
		t.Fatalf("uncertain entry was offered again: %+v %v %v", again, ok, err)
	}
}

func TestT766AttemptInFlightStillSerialisesTheSeat(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	first, _, _ := q.Append("a", "in flight", now)
	if _, _, err := q.Append("a", "must wait", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || attempt.ID != first.ID {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	blocker, ok, err := q.ClaimFront("a")
	if err != nil || ok || blocker.ID != first.ID {
		t.Fatalf("a second attempt started while one was in flight: %+v %v %v", blocker, ok, err)
	}
	if _, _, err := q.PopFront("a"); err == nil {
		t.Fatal("legacy pop took an entry while an attempt was in flight")
	}
}

func TestT766StaleAttemptOnNonHeadEntryIsRefused(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	q.Append("a", "ambiguous", now)
	q.Append("a", "behind it", now.Add(time.Second))
	a1, _, _ := q.ClaimFront("a")
	if err := q.Resolve("a", a1, Unverified, "unknown"); err != nil {
		t.Fatal(err)
	}
	a2, ok, err := q.ClaimFront("a")
	if err != nil || !ok {
		t.Fatalf("claim second: %v %v", ok, err)
	}
	stale := a2
	stale.AttemptID = NewID()
	if err := q.Resolve("a", stale, Confirmed, "late"); err == nil {
		t.Fatal("a stale attempt id resolved a non-head entry")
	}
	if err := q.Resolve("a", a2, Confirmed, "seen"); err != nil {
		t.Fatal(err)
	}
}

// An Attempting entry left by a daemon that died mid-submit is not a send in
// flight here: it is held and flowed past, never re-offered.
func TestT766OrphanedAttemptFromDeadDaemonIsFlowedPast(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	now := time.Now()
	first, _, _ := q.Append("a", "mid-submit when the daemon died", now)
	second, _, _ := q.Append("a", "behind it", now.Add(time.Second))
	if _, ok, err := q.ClaimFront("a"); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	after := NewStore(dir) // the restarted daemon owns no attempt
	next, ok, err := after.ClaimFront("a")
	if err != nil || !ok || next.ID != second.ID {
		t.Fatalf("orphaned attempt froze the queue: %+v %v %v", next, ok, err)
	}
	if held, blocked, err := after.BlockedHead("a"); err != nil || !blocked || held.ID != first.ID {
		t.Fatalf("orphaned attempt not reported held: %+v %v %v", held, blocked, err)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"strings"
	"testing"
	"time"
)

// 🎯T766.5: one delivery machine. The empty DeliveryState, an in-flight
// attempt, an orphaned attempt, an uncertain hold, a digest, and an operator
// requeue are phases of that machine. The JSON field names stay.

func TestT7665AdmittedIsThePendingPhase(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if _, _, err := q.Append("a", "accepted", now); err != nil {
		t.Fatal(err)
	}
	got, err := q.Snapshot("a")
	if err != nil || len(got) != 1 {
		t.Fatalf("snapshot: %+v %v", got, err)
	}
	if got[0].State != Pending || EntryPhase(got[0], "") != PhaseAdmitted {
		t.Fatalf("phase = %q state = %q; want admitted and the empty state", EntryPhase(got[0], ""), got[0].State)
	}
	b, err := q.Backlog("a")
	if err != nil || b.Depth != 1 || b.Uncertain != 0 || b.BlockReason != "" || b.OldestAge(now) != 0 {
		t.Fatalf("backlog = %+v %v", b, err)
	}
	if desc := b.Describe(now); !strings.Contains(desc, "1 queued") || strings.Contains(desc, "block:") {
		t.Fatalf("Describe = %q; want a pending queue with no block reason", desc)
	}
}

func TestT7665NotSentReturnsTheSameEntryToTheFront(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	first, _, err := q.Append("a", "refused", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := q.Append("a", "behind", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || attempt.ID != first.ID {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	if EntryPhase(attempt, attempt.AttemptID) != PhaseInFlight || attempt.State != Attempting {
		t.Fatalf("claimed phase = %s state = %q", EntryPhase(attempt, attempt.AttemptID), attempt.State)
	}
	again, ok, err := q.ClaimFront("a")
	if err != nil || ok || again.ID != first.ID {
		t.Fatalf("in-flight claim wedged or moved: %+v %v %v", again, ok, err)
	}
	if err := q.Resolve("a", attempt, DefinitelyNotSent, "cannot accept"); err != nil {
		t.Fatal(err)
	}
	left, err := q.Snapshot("a")
	if err != nil || len(left) != 2 || left[0].ID != first.ID || left[0].State != Pending || left[0].AttemptID != "" {
		t.Fatalf("not-sent did not return the same entry to the front: %+v %v", left, err)
	}
	if EntryPhase(left[0], "") != PhaseAdmitted {
		t.Fatalf("returned phase = %s; want admitted", EntryPhase(left[0], ""))
	}
	retry, ok, err := q.ClaimFront("a")
	if err != nil || !ok || retry.ID != first.ID || retry.AttemptID == attempt.AttemptID {
		t.Fatalf("retry = %+v %v %v", retry, ok, err)
	}
}

func TestT7665UncertainDoesNotWedgeAndRequeueIsPending(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	first, _, err := q.Append("a", "ambiguous", now)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := q.Resolve("a", attempt, Unverified, "no receiver evidence"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := q.Append("a", "behind the hold", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	held, err := q.Snapshot("a")
	if err != nil || EntryPhase(held[0], "") != PhaseUncertain || held[0].State != Uncertain {
		t.Fatalf("held = %+v %v", held, err)
	}
	next, ok, err := q.ClaimFront("a")
	if err != nil || !ok || next.Text != "behind the hold" {
		t.Fatalf("uncertain wedged the queue: %+v %v %v", next, ok, err)
	}
	if err := q.Resolve("a", next, Confirmed, "seen"); err != nil {
		t.Fatal(err)
	}
	b, err := q.Backlog("a")
	if err != nil || b.Depth != 1 || b.Uncertain != 1 || b.BlockReason != "uncertain" {
		t.Fatalf("backlog = %+v %v", b, err)
	}
	if desc := b.Describe(now); !strings.Contains(desc, "block: uncertain") || !strings.Contains(desc, "oldest waiting") {
		t.Fatalf("Describe = %q; want depth, age, and the block reason", desc)
	}
	rec, err := q.Reconcile("a", first.ID, attempt.AttemptID, ReconcileRequeue, "owner", "transcript has no copy", now)
	if err != nil || len(rec.Kept) != 1 || len(rec.Removed) != 0 {
		t.Fatalf("requeue = %+v %v", rec, err)
	}
	back, err := q.Snapshot("a")
	if err != nil || len(back) != 1 || back[0].ID != first.ID || back[0].State != Pending || back[0].Reconciled == nil {
		t.Fatalf("requeue = %+v %v", back, err)
	}
	if EntryPhase(back[0], "") != PhaseRequeued {
		t.Fatalf("phase = %s; want requeued on the empty state", EntryPhase(back[0], ""))
	}
	offered, ok, err := q.ClaimFront("a")
	if err != nil || !ok || offered.ID != first.ID {
		t.Fatalf("requeued entry was not offered: %+v %v %v", offered, ok, err)
	}
}

func TestT7665OrphanedAttemptIsHeldAndOperatorDropKeepsThePayload(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	now := time.Now()
	first, _, err := q.Append("a", "mid-submit", now)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if _, _, err := q.Append("a", "later", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(dir)
	rows, err := restarted.Snapshot("a")
	if err != nil || EntryPhase(rows[0], "") != PhaseHeldAttempt {
		t.Fatalf("orphan phase = %+v %v", rows, err)
	}
	next, ok, err := restarted.ClaimFront("a")
	if err != nil || !ok || next.Text != "later" {
		t.Fatalf("orphan wedged the queue: %+v %v %v", next, ok, err)
	}
	b, err := restarted.Backlog("a")
	if err != nil || b.BlockReason != "held attempt" {
		t.Fatalf("backlog = %+v %v", b, err)
	}
	rec, err := restarted.Reconcile("a", first.ID, attempt.AttemptID, ReconcileDrop, "owner", "pane is gone", now)
	if err != nil || len(rec.Removed) != 1 || rec.Removed[0].Text != "mid-submit" {
		t.Fatalf("drop = %+v %v", rec, err)
	}
	left, err := restarted.Snapshot("a")
	if err != nil || len(left) != 1 || left[0].ID == first.ID {
		t.Fatalf("drop destroyed the later entry or kept the held one: %+v %v", left, err)
	}
}

func TestT7665DigestIsOnePendingObligation(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	for i := 0; i <= DigestThreshold; i++ {
		if _, _, err := q.Append("a", "m", now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	digest, ok, err := q.ClaimDigest("a")
	if err != nil || !ok || digest.Members == "" || digest.State != Attempting {
		t.Fatalf("digest = %+v %v %v", digest, ok, err)
	}
	if EntryPhase(digest, digest.AttemptID) != PhaseInFlight {
		t.Fatalf("digest phase = %s; want in flight", EntryPhase(digest, digest.AttemptID))
	}
	if err := q.Resolve("a", digest, DefinitelyNotSent, "cannot accept"); err != nil {
		t.Fatal(err)
	}
	left, err := q.Snapshot("a")
	if err != nil || len(left) != 1 || left[0].ID != digest.ID || left[0].State != Pending || left[0].Members == "" {
		t.Fatalf("not-sent digest = %+v %v", left, err)
	}
	if EntryPhase(left[0], "") != PhaseDigest {
		t.Fatalf("phase = %s; want one digest obligation", EntryPhase(left[0], ""))
	}
	if _, _, err := q.Append("a", "after", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	retry, ok, err := q.ClaimFront("a")
	if err != nil || !ok || retry.ID != digest.ID {
		t.Fatalf("digest was not the one pending obligation: %+v %v %v", retry, ok, err)
	}
}

func TestT7665InFlightIsNotUncertain(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	first, _, err := q.Append("a", "owned attempt", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := q.Append("a", "behind the attempt", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || attempt.ID != first.ID {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	b, err := q.Backlog("a")
	if err != nil || b.Depth != 2 || b.Uncertain != 0 || b.InFlight != 1 || b.BlockReason != "in flight" {
		t.Fatalf("in-flight backlog = %+v %v", b, err)
	}
	if desc := b.Describe(now); !strings.Contains(desc, "2 queued") || !strings.Contains(desc, "oldest waiting") || !strings.Contains(desc, "block: in flight") {
		t.Fatalf("Describe = %q; want the queue, its age, and the in-flight block", desc)
	}
	if err := q.Resolve("a", attempt, Unverified, "no receiver evidence"); err != nil {
		t.Fatal(err)
	}
	after, err := q.Backlog("a")
	if err != nil || after.Uncertain != 1 || after.InFlight != 0 || after.BlockReason != "uncertain" {
		t.Fatalf("after unverified = %+v %v", after, err)
	}
}

func TestT7665PopFrontRequiresAnOperatorReceipt(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if _, _, err := q.Append("a", "accepted payload", now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := q.PopFront("a", "", "", now); err == nil || ok {
		t.Fatalf("blank actor pop = ok %v err %v", ok, err)
	}
	if _, ok, err := q.PopFront("a", "operator", " ", now); err == nil || ok {
		t.Fatalf("blank evidence pop = ok %v err %v", ok, err)
	}
	left, err := q.Snapshot("a")
	if err != nil || len(left) != 1 || left[0].Text != "accepted payload" {
		t.Fatalf("blank pop removed the entry: %+v %v", left, err)
	}
	if _, err := q.Reconcile("a", left[0].ID, "", ReconcilePop, "operator", "a pop is not a reconcile", now); err == nil {
		t.Fatal("reconcile accepted a pop")
	}
	still, err := q.Snapshot("a")
	if err != nil || len(still) != 1 || still[0].Text != "accepted payload" {
		t.Fatalf("rejected pop changed the queue: %+v %v", still, err)
	}
	rec, ok, err := q.PopFront("a", "operator", "owner took the payload", now)
	if err != nil || !ok || len(rec.Removed) != 1 || rec.Removed[0].Text != "accepted payload" {
		t.Fatalf("pop = %+v %v %v", rec, ok, err)
	}
	if rec.By.Actor != "operator" || rec.By.Evidence != "owner took the payload" || rec.By.Outcome != ReconcilePop || !rec.By.At.Equal(now.UTC()) {
		t.Fatalf("receipt by = %+v", rec.By)
	}
	gone, err := q.Snapshot("a")
	if err != nil || len(gone) != 0 {
		t.Fatalf("queue after pop = %+v %v", gone, err)
	}
	empty, ok, err := q.PopFront("a", "operator", "nothing left", now)
	if err != nil || ok || len(empty.Removed) != 0 {
		t.Fatalf("empty pop = %+v %v %v", empty, ok, err)
	}
}

// A predicate that matches every entry must still leave a digest and an
// operator requeue in the queue. Those phases share the empty on-disk state
// with an admitted entry. Only the admitted payload moves, and it moves into
// the supersession archive rather than disappearing.
func TestT7665SupersedeRecordsAdmittedAndLeavesOtherPhases(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	operator, _, err := q.Append("a", "operator sent this back", now)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok {
		t.Fatalf("claim operator: %+v %v %v", attempt, ok, err)
	}
	if err := q.Resolve("a", attempt, Unverified, "no receiver evidence"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= DigestThreshold; i++ {
		if _, _, err := q.Append("a", "folded member", now.Add(time.Duration(i+1)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	digest, ok, err := q.ClaimDigest("a")
	if err != nil || !ok || digest.Members == "" {
		t.Fatalf("digest = %+v %v %v", digest, ok, err)
	}
	if err := q.Resolve("a", digest, DefinitelyNotSent, "cannot accept"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Reconcile("a", operator.ID, attempt.AttemptID, ReconcileRequeue, "owner", "transcript has no copy", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := q.Append("a", "ordinary admitted", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	newest, _, replaced, err := q.AppendSuperseding("a", "newest", now.Add(2*time.Minute), func(Entry) bool { return true })
	if err != nil || replaced != 1 {
		t.Fatalf("replaced = %d err %v", replaced, err)
	}
	left, err := q.Snapshot("a")
	if err != nil {
		t.Fatal(err)
	}
	var sawOperator, sawDigest, sawNewest bool
	for _, e := range left {
		switch e.ID {
		case operator.ID:
			sawOperator = e.Text == "operator sent this back" && EntryPhase(e, "") == PhaseRequeued
		case digest.ID:
			sawDigest = e.Members == digest.Members && EntryPhase(e, "") == PhaseDigest
		case newest.ID:
			sawNewest = e.Text == "newest"
		default:
			t.Fatalf("unexpected entry left in the queue: %+v", e)
		}
	}
	if !sawOperator || !sawDigest || !sawNewest {
		t.Fatalf("queue = %+v", left)
	}
	archived, err := q.ReadSupersession(newest.ID)
	if err != nil || len(archived.Removed) != 1 || archived.Removed[0].Text != "ordinary admitted" {
		t.Fatalf("archive = %+v %v", archived, err)
	}
}

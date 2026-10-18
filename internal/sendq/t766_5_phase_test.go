// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"os"
	"path/filepath"
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

func TestT7665ClaimIDTakesALaterClaimableAndLeavesTheHead(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	head, _, err := q.Append("a", "gate feedback stays", now)
	if err != nil {
		t.Fatal(err)
	}
	tail, _, err := q.Append("a", "follow-up", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := q.ClaimFrontID("a", tail.ID); err != nil || claimed {
		t.Fatalf("ClaimFrontID took the later id: claimed=%v err=%v", claimed, err)
	}
	got, ok, err := q.ClaimID("a", tail.ID)
	if err != nil || !ok || got.ID != tail.ID || got.State != Attempting {
		t.Fatalf("ClaimID = %+v %v %v", got, ok, err)
	}
	left, err := q.Snapshot("a")
	if err != nil || len(left) != 2 || left[0].ID != head.ID || left[0].State != Pending || left[1].ID != tail.ID || left[1].State != Attempting {
		t.Fatalf("head was not left in place: %+v %v", left, err)
	}
	if _, ok, err := q.ClaimID("a", head.ID); err != nil || ok {
		t.Fatalf("in-flight seat allowed another claim: ok=%v err=%v", ok, err)
	}
	if err := q.Resolve("a", got, Unverified, "parent accepted the call without a receipt"); err != nil {
		t.Fatal(err)
	}
	left, err = q.Snapshot("a")
	if err != nil || len(left) != 2 || left[0].State != Pending || left[0].Text != "gate feedback stays" || left[1].State != Uncertain {
		t.Fatalf("after the later handoff: %+v %v", left, err)
	}
	if _, err := q.ReadSupersession(head.ID); err == nil {
		t.Fatal("ClaimID archived the head")
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
	if _, err := q.ReadSupersession(left[0].ID); err == nil {
		t.Fatal("blank pop archived the payload")
	}
	if _, err := q.Reconcile("a", left[0].ID, "", ReconcilePop, "operator", "a pop is not a reconcile", now); err == nil {
		t.Fatal("reconcile accepted a pop")
	}
	still, err := q.Snapshot("a")
	if err != nil || len(still) != 1 || still[0].Text != "accepted payload" {
		t.Fatalf("rejected pop changed the queue: %+v %v", still, err)
	}
	poppedID := left[0].ID
	rec, ok, err := q.PopFront("a", "operator", "owner took the payload", now)
	if err != nil || !ok || len(rec.Removed) != 1 || rec.Removed[0].Text != "accepted payload" {
		t.Fatalf("pop = %+v %v %v", rec, ok, err)
	}
	archived, err := q.ReadSupersession(poppedID)
	if err != nil || len(archived.Removed) != 1 || archived.Removed[0].ID != poppedID || archived.Removed[0].Text != "accepted payload" {
		t.Fatalf("pop archive = %+v %v", archived, err)
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

// DischargePending is the receiver-has-it exit. A matched admitted copy and a
// matched operator requeue leave the queue only after their payloads are
// archived. An uncertain entry stays even when the predicate matches it.
func TestT7665DischargeArchivesMatchedAndRequeued(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	uncertain, _, err := q.Append("a", "uncertain copy", now)
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || attempt.ID != uncertain.ID {
		t.Fatalf("claim uncertain: %+v %v %v", attempt, ok, err)
	}
	if err := q.Resolve("a", attempt, Unverified, "no receiver evidence"); err != nil {
		t.Fatal(err)
	}
	requeued, _, err := q.Append("a", "requeued copy", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	second, ok, err := q.ClaimFront("a")
	if err != nil || !ok || second.ID != requeued.ID {
		t.Fatalf("claim requeue: %+v %v %v", second, ok, err)
	}
	if err := q.Resolve("a", second, Unverified, "no receiver evidence"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Reconcile("a", requeued.ID, second.AttemptID, ReconcileRequeue, "owner", "transcript has no copy", now); err != nil {
		t.Fatal(err)
	}
	admitted, _, err := q.Append("a", "admitted copy", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := q.Append("a", "still waiting", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	removed, err := q.DischargePending("a", func(e Entry) bool {
		return e.Text == "requeued copy" || e.Text == "admitted copy" || e.Text == "uncertain copy"
	})
	if err != nil || removed != 2 {
		t.Fatalf("removed = %d err %v", removed, err)
	}
	left, err := q.Snapshot("a")
	if err != nil {
		t.Fatal(err)
	}
	var sawUncertain, sawWaiting bool
	for _, e := range left {
		switch e.ID {
		case uncertain.ID:
			sawUncertain = e.Text == "uncertain copy" && EntryPhase(e, "") == PhaseUncertain
		case requeued.ID, admitted.ID:
			t.Fatalf("discharged entry still queued: %+v", e)
		default:
			sawWaiting = e.Text == "still waiting" && EntryPhase(e, "") == PhaseAdmitted
		}
	}
	if !sawUncertain || !sawWaiting || len(left) != 2 {
		t.Fatalf("queue = %+v", left)
	}
	for _, id := range []string{requeued.ID, admitted.ID} {
		got, err := q.ReadSupersession(id)
		if err != nil || len(got.Removed) != 1 || got.Removed[0].ID != id {
			t.Fatalf("archive %s = %+v %v", id, got, err)
		}
	}
	requeuedArchive, err := q.ReadSupersession(requeued.ID)
	if err != nil || requeuedArchive.Removed[0].Text != "requeued copy" || requeuedArchive.Removed[0].Reconciled == nil {
		t.Fatalf("requeued archive = %+v %v", requeuedArchive, err)
	}
	if _, err := q.ReadSupersession(uncertain.ID); err == nil {
		t.Fatal("uncertain entry was archived")
	}
}

// Confirmed and TerminalUndelivered are the Resolve exits that drop the
// entry. The payload is archived under that entry's id. Unverified and
// DefinitelyNotSent keep the entry and write no archive. Deleting the
// archive call on the remove path leaves the queue empty and fails the
// confirmed and terminal reads. Archiving the non-remove path fails the
// unverified and not-sent reads.
func TestT7665ResolveArchivesOnlyTheDroppedEntry(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	confirmed, _, err := q.Append("a", "confirmed copy", now)
	if err != nil {
		t.Fatal(err)
	}
	confirmedAttempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || confirmedAttempt.ID != confirmed.ID {
		t.Fatalf("claim confirmed: %+v %v %v", confirmedAttempt, ok, err)
	}
	if err := q.Resolve("a", confirmedAttempt, Confirmed, "receipt accepted"); err != nil {
		t.Fatal(err)
	}

	terminal, _, err := q.Append("a", "terminal copy", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	terminalAttempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || terminalAttempt.ID != terminal.ID {
		t.Fatalf("claim terminal: %+v %v %v", terminalAttempt, ok, err)
	}
	if err := q.Resolve("a", terminalAttempt, TerminalUndelivered, "addressee gone"); err != nil {
		t.Fatal(err)
	}

	unverified, _, err := q.Append("a", "unverified copy", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	unverifiedAttempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || unverifiedAttempt.ID != unverified.ID {
		t.Fatalf("claim unverified: %+v %v %v", unverifiedAttempt, ok, err)
	}
	if err := q.Resolve("a", unverifiedAttempt, Unverified, "no receiver evidence"); err != nil {
		t.Fatal(err)
	}

	notSent, _, err := q.Append("a", "not sent copy", now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	notSentAttempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok || notSentAttempt.ID != notSent.ID {
		t.Fatalf("claim not-sent: %+v %v %v", notSentAttempt, ok, err)
	}
	if err := q.Resolve("a", notSentAttempt, DefinitelyNotSent, "cannot accept"); err != nil {
		t.Fatal(err)
	}

	left, err := q.Snapshot("a")
	if err != nil {
		t.Fatal(err)
	}
	var sawUnverified, sawNotSent bool
	for _, e := range left {
		switch e.ID {
		case confirmed.ID, terminal.ID:
			t.Fatalf("dropped entry still queued: %+v", e)
		case unverified.ID:
			sawUnverified = e.Text == "unverified copy" && EntryPhase(e, "") == PhaseUncertain
		case notSent.ID:
			sawNotSent = e.Text == "not sent copy" && EntryPhase(e, "") == PhaseAdmitted
		default:
			t.Fatalf("unexpected entry: %+v", e)
		}
	}
	if !sawUnverified || !sawNotSent || len(left) != 2 {
		t.Fatalf("queue = %+v", left)
	}
	for _, want := range []struct {
		id, text string
	}{
		{confirmed.ID, "confirmed copy"},
		{terminal.ID, "terminal copy"},
	} {
		got, err := q.ReadSupersession(want.id)
		if err != nil || len(got.Removed) != 1 || got.Removed[0].ID != want.id || got.Removed[0].Text != want.text {
			t.Fatalf("archive %s = %+v %v", want.id, got, err)
		}
	}
	for _, id := range []string{unverified.ID, notSent.ID} {
		if _, err := q.ReadSupersession(id); err == nil {
			t.Fatalf("kept entry %s was archived", id)
		}
	}
}

// An on-disk entry can omit state and still carry an attempt id. That shape
// is not the claimable pending. The loader accepts it. The exit is an
// operator requeue that names the attempt, not a JSON edit.
func TestT7665EmptyStateWithAttemptIDIsNotPending(t *testing.T) {
	dir := t.TempDir()
	const raw = `{"agent":"a","entries":[{"id":"e1","text":"kept","enqueued_at":"2026-10-19T12:00:00Z","attempt_id":"att-1","directive":{"family":"job","kind":"authorization"}}]}`
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	q := NewStore(dir)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	rows, err := q.Snapshot("a")
	if err != nil || len(rows) != 1 {
		t.Fatalf("load: %+v %v", rows, err)
	}
	e := rows[0]
	if e.State != Pending || e.AttemptID != "att-1" || e.Text != "kept" || EntryPhase(e, "") != PhaseHeldAttempt {
		t.Fatalf("phase = %s state = %q attempt = %q text = %q", EntryPhase(e, ""), e.State, e.AttemptID, e.Text)
	}
	if EntryPhase(e, "att-1") != PhaseInFlight {
		t.Fatalf("owned phase = %s; want in flight", EntryPhase(e, "att-1"))
	}
	claimed, ok, err := q.ClaimFront("a")
	if err != nil || ok || claimed.Text != "kept" || claimed.State != Pending || claimed.AttemptID != "att-1" {
		t.Fatalf("claim treated the leftover as pending: %+v %v %v", claimed, ok, err)
	}
	b, err := q.Backlog("a")
	if err != nil || b.Pending != 0 || b.Depth != 1 || b.InFlight != 0 || b.Uncertain != 1 || b.BlockReason != "held attempt" {
		t.Fatalf("backlog = %+v %v", b, err)
	}
	if removed, err := q.DischargePending("a", func(Entry) bool { return true }); err != nil || removed != 0 {
		t.Fatalf("discharge removed %d err %v", removed, err)
	}
	if _, _, err := q.Append("a", "other", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Consolidate("a", "", "folded", "owner", "must not fold a leftover attempt", now); err == nil {
		t.Fatal("consolidate folded a leftover attempt")
	}
	if _, _, _, err := q.ApplyDirective("a", "stop", Directive{Family: "job", Kind: "hold"}, true, now); err == nil {
		t.Fatal("hold removed a leftover authorization")
	}
	for i := 0; i < DigestThreshold; i++ {
		if _, _, err := q.Append("a", "fold me", now.Add(time.Duration(i+2)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	digest, ok, err := q.ClaimDigest("a")
	if err != nil || !ok {
		t.Fatalf("digest = %+v %v %v", digest, ok, err)
	}
	rows, err = q.Snapshot("a")
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, row := range rows {
		if row.ID != "e1" {
			continue
		}
		saw = row.Text == "kept" && row.State == Pending && row.AttemptID == "att-1" && EntryPhase(row, "") == PhaseHeldAttempt
	}
	if !saw {
		t.Fatalf("digest destroyed the leftover: %+v", rows)
	}
	if _, err := q.ReadArchived("e1"); err == nil {
		t.Fatal("digest archived the leftover")
	}
	if _, err := q.ReadSupersession("e1"); err == nil {
		t.Fatal("a daemon path archived the leftover")
	}
	if _, err := q.Reconcile("a", "e1", "", ReconcileRequeue, "owner", "file omitted state", now); err == nil {
		t.Fatal("requeue without the attempt id was accepted")
	}
	rec, err := q.Reconcile("a", "e1", "att-1", ReconcileRequeue, "owner", "file omitted state", now)
	if err != nil || len(rec.Kept) != 1 || rec.Kept[0].AttemptID != "" || rec.Kept[0].State != Pending {
		t.Fatalf("requeue = %+v %v", rec, err)
	}
	rows, err = q.Snapshot("a")
	if err != nil {
		t.Fatal(err)
	}
	var back Entry
	for _, row := range rows {
		if row.ID == "e1" {
			back = row
		}
	}
	if back.ID != "e1" || back.AttemptID != "" || back.State != Pending || back.Text != "kept" || EntryPhase(back, "") != PhaseRequeued {
		t.Fatalf("after requeue = %+v", back)
	}
	if err := q.Resolve("a", digest, Confirmed, "folded"); err != nil {
		t.Fatal(err)
	}
	offered, ok, err := q.ClaimFront("a")
	if err != nil || !ok || offered.ID != "e1" {
		t.Fatalf("requeued leftover was not offered: %+v %v %v", offered, ok, err)
	}
}

// The same omitted state is in flight when this store already owns the attempt id.
func TestT7665EmptyStateOwnedAttemptIsInFlight(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	first, _, err := q.Append("a", "kept", now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := q.ClaimFront("a")
	if err != nil || !ok || claimed.ID != first.ID || claimed.AttemptID == "" {
		t.Fatalf("claim: %+v %v %v", claimed, ok, err)
	}
	body := []byte(`{"agent":"a","entries":[{"id":"` + first.ID + `","text":"kept","enqueued_at":"2026-10-19T12:00:00Z","attempt_id":"` + claimed.AttemptID + `"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "a.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := q.Backlog("a")
	if err != nil || b.Pending != 0 || b.Depth != 1 || b.InFlight != 1 || b.BlockReason != "in flight" {
		t.Fatalf("owned backlog = %+v %v", b, err)
	}
	rows, err := q.Snapshot("a")
	if err != nil || len(rows) != 1 || rows[0].State != Pending || rows[0].AttemptID != claimed.AttemptID || rows[0].Text != "kept" {
		t.Fatalf("rewritten row = %+v %v", rows, err)
	}
	if EntryPhase(rows[0], claimed.AttemptID) != PhaseInFlight {
		t.Fatalf("phase = %s", EntryPhase(rows[0], claimed.AttemptID))
	}
	again, ok, err := q.ClaimFront("a")
	if err != nil || ok {
		t.Fatalf("claim of an owned empty state: %+v %v %v", again, ok, err)
	}
	fresh := NewStore(dir)
	held, err := fresh.Backlog("a")
	if err != nil || held.Pending != 0 || held.Depth != 1 || held.InFlight != 0 || held.BlockReason != "held attempt" {
		t.Fatalf("fresh backlog = %+v %v", held, err)
	}
}

// The same owned empty state is not an orphan. Reconcile must not confirm,
// requeue, or drop it out from under the attempt this store still owns.
func TestT7665OwnedOmittedStateIsNotReconcilable(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	first, _, err := q.Append("a", "kept", now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := q.ClaimFront("a")
	if err != nil || !ok || claimed.AttemptID == "" {
		t.Fatalf("claim: %+v %v %v", claimed, ok, err)
	}
	body := []byte(`{"agent":"a","entries":[{"id":"` + first.ID + `","text":"kept","enqueued_at":"2026-10-19T12:00:00Z","attempt_id":"` + claimed.AttemptID + `"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "a.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []ReconcileOutcome{ReconcileConfirmed, ReconcileRequeue, ReconcileDrop} {
		_, err := q.Reconcile("a", first.ID, claimed.AttemptID, outcome, "owner", "transcript has no copy", now)
		if err == nil || !strings.Contains(err.Error(), "ACTIVE") {
			t.Fatalf("%s err = %v, want ACTIVE", outcome, err)
		}
	}
	rows, err := q.Snapshot("a")
	if err != nil || len(rows) != 1 || rows[0].Text != "kept" || rows[0].State != Pending || rows[0].AttemptID != claimed.AttemptID || EntryPhase(rows[0], claimed.AttemptID) != PhaseInFlight {
		t.Fatalf("reconcile changed the owned attempt: %+v %v", rows, err)
	}
}

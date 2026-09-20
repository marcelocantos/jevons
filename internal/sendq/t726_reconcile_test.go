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

// 🎯T726 — the store half. The failure being fixed is a hand-edit of
// ~/.jevons/sendq/*.json under a running daemon, so every oracle here asks
// the same question the editor could not answer: after this call, is the
// payload either still on disk or written down somewhere?

// uncertainQueue is the state claudia-po was stuck in: one accepted message,
// one attempt whose outcome nobody knows, and no live owner for it.
func uncertainQueue(t *testing.T, dir, agent, text string) (*Store, Entry) {
	t.Helper()
	s := NewStore(dir)
	if _, _, err := s.Append(agent, text, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.ClaimFront(agent)
	if err != nil || !ok {
		t.Fatalf("claim: %v ok=%v", err, ok)
	}
	if err := s.Resolve(agent, attempt, Unverified, "provider errored after write"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Snapshot(agent)
	if err != nil || len(entries) != 1 || entries[0].State != Uncertain {
		t.Fatalf("fixture is not uncertain: %+v %v", entries, err)
	}
	return s, entries[0]
}

func TestT726UncertainEntryResolvesToConfirmed(t *testing.T) {
	const agent, payload = "claudia-po", "owner: the bounce message"
	s, held := uncertainQueue(t, t.TempDir(), agent, payload)

	rec, err := s.Reconcile(agent, held.ID, held.AttemptID, ReconcileConfirmed,
		"jevons-po", "session f1a44c7f: queue-operation enqueue 15:25:32Z, remove 15:26:12Z", time.Now())
	if err != nil {
		t.Fatalf("reconcile refused a resolvable entry: %v", err)
	}
	if len(rec.Removed) != 1 || rec.Removed[0].Text != payload || rec.Depth != 0 {
		t.Fatalf("receipt does not account for the payload: %+v", rec)
	}
	if rec.By.Actor != "jevons-po" || !strings.Contains(rec.By.Evidence, "queue-operation") {
		t.Fatalf("disposition lost its attribution: %+v", rec.By)
	}
	entries, err := s.Snapshot(agent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("confirmed entry still held: %+v %v", entries, err)
	}
	// Clause 4's precondition: nothing is left for BlockedHead to pin on.
	if e, blocked, err := s.BlockedHead(agent); err != nil || blocked {
		t.Fatalf("seat is still blocked after reconciliation: %+v %v %v", e, blocked, err)
	}
}

func TestT726RequeueRestoresDeliveryAndOutlivesTheDaemon(t *testing.T) {
	dir := t.TempDir()
	const agent, payload = "jv-t718-gate-dirty-warn", "PO: bounce and re-read HEAD"
	s, held := uncertainQueue(t, dir, agent, payload)

	if _, err := s.Reconcile(agent, held.ID, held.AttemptID, ReconcileRequeue,
		"owner", "receiver's JSONL has no user message and no queue-operation record for this payload", time.Now()); err != nil {
		t.Fatalf("requeue refused: %v", err)
	}
	// The attribution is durable: the next daemon reads who decided and why,
	// which is the whole difference from an editor.
	reopened := NewStore(dir)
	entries, err := reopened.Snapshot(agent)
	if err != nil || len(entries) != 1 {
		t.Fatalf("requeued entry lost: %+v %v", entries, err)
	}
	e := entries[0]
	if e.State != Pending || e.AttemptID != "" || e.Text != payload || !e.EnqueuedAt.Equal(held.EnqueuedAt) {
		t.Fatalf("requeue did not return the entry to ordinary delivery: %+v", e)
	}
	if e.Reconciled == nil || e.Reconciled.Actor != "owner" || e.Reconciled.Outcome != ReconcileRequeue {
		t.Fatalf("requeue left no durable record of who decided: %+v", e.Reconciled)
	}
	// And the ordinary drain may now have it — the one outcome that permits a
	// resend, because non-delivery was established rather than assumed.
	if got, ok, err := reopened.ClaimFront(agent); err != nil || !ok || got.Text != payload {
		t.Fatalf("requeued entry is not claimable: %+v ok=%v %v", got, ok, err)
	}
}

// The target's RED clause: no mutation may drop an unconfirmed payload
// silently. Either the call is refused and the file is byte-identical, or it
// returns the payload in full for the caller to record.
func TestT726NoMutationSilentlyDropsAnUnconfirmedPayload(t *testing.T) {
	const agent, payload = "claudia-po", "the message whose fate is unknown"

	refusals := []struct {
		name string
		call func(*Store, Entry) (Receipt, error)
	}{
		{"anonymous disposition", func(s *Store, e Entry) (Receipt, error) {
			return s.Reconcile(agent, e.ID, e.AttemptID, ReconcileDrop, "  ", "looked at the transcript", time.Now())
		}},
		{"unevidenced disposition", func(s *Store, e Entry) (Receipt, error) {
			return s.Reconcile(agent, e.ID, e.AttemptID, ReconcileConfirmed, "jevons-po", "", time.Now())
		}},
		{"attempt not named", func(s *Store, e Entry) (Receipt, error) {
			return s.Reconcile(agent, e.ID, "", ReconcileConfirmed, "jevons-po", "read the queue records", time.Now())
		}},
		{"stale attempt id", func(s *Store, e Entry) (Receipt, error) {
			return s.Reconcile(agent, e.ID, "0000deadbeef", ReconcileConfirmed, "jevons-po", "read the queue records", time.Now())
		}},
		{"unknown entry id", func(s *Store, e Entry) (Receipt, error) {
			return s.Reconcile(agent, "ffffffffffff", e.AttemptID, ReconcileDrop, "jevons-po", "read the queue records", time.Now())
		}},
		{"invented outcome", func(s *Store, e Entry) (Receipt, error) {
			return s.Reconcile(agent, e.ID, e.AttemptID, ReconcileOutcome("probably_fine"), "jevons-po", "vibes", time.Now())
		}},
		{"requeue of an already-pending entry", func(s *Store, e Entry) (Receipt, error) {
			if _, _, err := s.Append(agent, "a later pending message", time.Now()); err != nil {
				t.Fatal(err)
			}
			entries, err := s.Snapshot(agent)
			if err != nil {
				t.Fatal(err)
			}
			return s.Reconcile(agent, entries[1].ID, "", ReconcileRequeue, "jevons-po", "nothing to establish", time.Now())
		}},
		{"consolidation over an unresolved attempt", func(s *Store, e Entry) (Receipt, error) {
			if _, _, err := s.Append(agent, "a later pending message", time.Now()); err != nil {
				t.Fatal(err)
			}
			return s.Consolidate(agent, "", "", "jevons-po", "the later message supersedes", time.Now())
		}},
	}
	for _, tc := range refusals {
		t.Run("refused: "+tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s, held := uncertainQueue(t, dir, agent, payload)
			if _, err := tc.call(s, held); err == nil {
				t.Fatal("mutation was accepted without a recorded, evidenced disposition")
			}
			entries, err := s.Snapshot(agent)
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, e := range entries {
				if e.ID == held.ID && e.Text == payload && e.State == Uncertain && e.AttemptID == held.AttemptID {
					found = true
				}
			}
			if !found {
				t.Fatalf("refused mutation still disturbed the unconfirmed payload: %+v", entries)
			}
			// And the refusal did not touch the file at all: a partial write
			// under a running daemon is the failure this tool replaces.
			raw, err := os.ReadFile(filepath.Join(dir, agent+".json"))
			if err != nil || !strings.Contains(string(raw), payload) || !strings.Contains(string(raw), held.AttemptID) {
				t.Fatalf("queue record damaged by a refused call: %v %s", err, raw)
			}
		})
	}

	for _, outcome := range []ReconcileOutcome{ReconcileConfirmed, ReconcileDrop} {
		t.Run("recorded: "+string(outcome), func(t *testing.T) {
			s, held := uncertainQueue(t, t.TempDir(), agent, payload)
			rec, err := s.Reconcile(agent, held.ID, held.AttemptID, outcome, "jevons-po", "read the receiver's queue records", time.Now())
			if err != nil {
				t.Fatalf("%s refused: %v", outcome, err)
			}
			if len(rec.Removed) != 1 || rec.Removed[0].Text != payload || rec.RemovedBytes() != len(payload) {
				t.Fatalf("%s removed a payload the caller cannot record: %+v", outcome, rec)
			}
			if rec.By.Describe() == "" || !strings.Contains(rec.By.Describe(), "jevons-po") {
				t.Fatalf("%s has no describable disposition: %+v", outcome, rec.By)
			}
		})
	}
}

// 🎯T623: a submission this daemon is still inside is not an orphan, and must
// not be reconciled out from under the drain that owns it.
func TestT726ActiveSubmissionIsNotReconcilable(t *testing.T) {
	const agent = "jv-worker"
	s := NewStore(t.TempDir())
	if _, _, err := s.Append(agent, "mid-flight", time.Now()); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.ClaimFront(agent)
	if err != nil || !ok {
		t.Fatalf("claim: %v ok=%v", err, ok)
	}
	if _, err := s.Reconcile(agent, attempt.ID, attempt.AttemptID, ReconcileConfirmed,
		"jevons-po", "assumed it landed", time.Now()); err == nil {
		t.Fatal("reconciled an actively owned submission")
	}
	entries, err := s.Snapshot(agent)
	if err != nil || len(entries) != 1 || entries[0].State != Attempting {
		t.Fatalf("active claim disturbed: %+v %v", entries, err)
	}
}

func TestT726SupersededMessagesFoldIntoOneAuthoritativeMessage(t *testing.T) {
	const agent = "ge-t191-ndk-discovery"
	s := NewStore(t.TempDir())
	base := time.Now().Add(-3 * time.Hour)
	var ids []string
	for i, text := range []string{"first instruction", "second instruction", "third instruction"} {
		e, _, err := s.Append(agent, text, base.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	rec, err := s.Consolidate(agent, ids[2], "authoritative: do the third thing only",
		"jevons-po", "the first two are superseded by the third", time.Now())
	if err != nil {
		t.Fatalf("consolidate refused: %v", err)
	}
	entries, err := s.Snapshot(agent)
	if err != nil || len(entries) != 1 {
		t.Fatalf("receiver would still get %d messages: %+v %v", len(entries), entries, err)
	}
	got := entries[0]
	if got.ID != ids[2] || got.Text != "authoritative: do the third thing only" {
		t.Fatalf("survivor is not the authoritative message: %+v", got)
	}
	// The age a stall is visible in must not be reset by housekeeping.
	if !got.EnqueuedAt.Equal(base) {
		t.Fatalf("consolidation reset the wait: %s want %s", got.EnqueuedAt, base)
	}
	if got.Reconciled == nil || got.Reconciled.Outcome != ReconcileConsolidate {
		t.Fatalf("survivor carries no record of the fold: %+v", got.Reconciled)
	}
	if len(rec.Removed) != 2 || rec.Removed[0].Text != "first instruction" || rec.Removed[1].Text != "second instruction" {
		t.Fatalf("superseded payloads not handed back for recording: %+v", rec.Removed)
	}
}

func TestT726ConsolidateNeedsSomethingToConsolidate(t *testing.T) {
	const agent = "jv-worker"
	s := NewStore(t.TempDir())
	if _, _, err := s.Append(agent, "only message", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consolidate(agent, "", "", "jevons-po", "tidying", time.Now()); err == nil {
		t.Fatal("consolidated a single-message queue")
	}
	if entries, err := s.Snapshot(agent); err != nil || len(entries) != 1 {
		t.Fatalf("no-op consolidation disturbed the queue: %+v %v", entries, err)
	}
}

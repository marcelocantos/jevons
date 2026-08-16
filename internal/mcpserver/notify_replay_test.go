// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// 🎯T428 oracles. Every one of these is red on the pre-fix tree, where
// deliverToOverseer handed the seam whatever it was given, however many times
// it was given it.
//
// The specimen they are built from: overseer session ca276b85, 2026-08-10. One
// batch — sentinel snapshot + a closed jv-t370-auto impatience incident + RSI
// coach judgment 44c736552294ab9e02e8c3a01dcca666 — delivered FOUR times
// byte-identically inside a single running turn.

const t428Batch = `[Fleet health] sentinel snapshot
jv-t370-auto: impatience incident closed
RSI judgment fingerprint 44c736552294ab9e02e8c3a01dcca666`

// t428Server is the deliver path with the seam recorded and the overseer's
// records saying the batch arrived — the strongest confirmation there is, and
// the one the observed replay had available and ignored.
func t428Server(t *testing.T, ev TurnEvidence) (*Server, *overseerInbox) {
	t.Helper()
	inbox := &overseerInbox{}
	s := &Server{}
	s.SetOverseerDeliver(inbox.deliver)
	s.SetTurnWitness(witnessYielding(ev))
	return s, inbox
}

// THE CENTRAL CLAIM: one batch, one delivery, however many times the sources
// offer it while the turn that received it is still running.
func TestT428IdenticalBatchDeliveredOnce(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})

	res, err := s.deliverByName("jevons", t428Batch, OriginAgent, false)
	if err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if res.Status != "sent" {
		t.Fatalf("first delivery status=%q want sent", res.Status)
	}

	// The three replays the overseer actually received.
	for i := 2; i <= 4; i++ {
		res, err := s.deliverByName("jevons", t428Batch, OriginAgent, false)
		if err != nil {
			// Not an error: deliverSentinel falls back to butler.PushEvent on
			// error, which would deliver the copy this guard withholds.
			t.Fatalf("replay %d returned an error: %v", i, err)
		}
		if res.Status != StatusSuppressedReplay {
			t.Fatalf("replay %d status=%q want %q", i, res.Status, StatusSuppressedReplay)
		}
		if !strings.Contains(res.Message, "🎯T428") {
			t.Fatalf("replay %d message does not name the guard: %q", i, res.Message)
		}
	}

	if len(inbox.texts) != 1 {
		t.Fatalf("overseer received %d copies, want exactly 1: %v", len(inbox.texts), inbox.texts)
	}
}

// A recurring condition whose report CHANGED is the channel doing its job.
// This is the mutation that a lazy fix (suppress everything from a source for
// a while) would fail, and it is why the digest is over the whole batch.
func TestT428ChangedBatchStillDelivers(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})

	for i, text := range []string{
		t428Batch,
		t428Batch + "\njv-t374: 2 incidents",
		t428Batch + "\njv-t374: 3 incidents",
	} {
		res, err := s.deliverByName("jevons", text, OriginAgent, false)
		if err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
		if res.Status == StatusSuppressedReplay {
			t.Fatalf("delivery %d suppressed changed content: %s", i, res.Message)
		}
	}
	if len(inbox.texts) != 3 {
		t.Fatalf("overseer received %d copies, want 3: %v", len(inbox.texts), inbox.texts)
	}
	// Trailing whitespace is not new content.
	res, err := s.deliverByName("jevons", "  "+t428Batch+"\n\n", OriginAgent, false)
	if err != nil {
		t.Fatalf("whitespace variant: %v", err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("whitespace variant status=%q want suppressed", res.Status)
	}
}

// THE OWNER MAY REPEAT THEMSELVES AND MEAN IT. Swallowing an owner turn would
// be a worse defect than the one being fixed.
func TestT428OwnerTurnsAreNeverDeduped(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})

	for i := range 3 {
		res, err := s.deliverByName("jevons", "status?", OriginOwner, false)
		if err != nil {
			t.Fatalf("owner turn %d: %v", i, err)
		}
		if res.Status == StatusSuppressedReplay {
			t.Fatalf("owner turn %d was suppressed as a replay", i)
		}
	}
	if len(inbox.texts) != 3 {
		t.Fatalf("owner said it 3 times, overseer got %d", len(inbox.texts))
	}
}

// A DELIVERY THAT FAILED IS NOT A DELIVERY. The guard must not turn one
// transport failure into permanent silence about the condition.
func TestT428FailedDeliveryStaysRetryable(t *testing.T) {
	inbox := &overseerInbox{err: fmt.Errorf("notify queue closed")}
	s := &Server{}
	s.SetOverseerDeliver(inbox.deliver)
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))

	if _, err := s.deliverByName("jevons", t428Batch, OriginAgent, false); err == nil {
		t.Fatal("want an error from the failing seam")
	}

	inbox.err = nil
	res, err := s.deliverByName("jevons", t428Batch, OriginAgent, false)
	if err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if res.Status == StatusSuppressedReplay {
		t.Fatalf("retry after a FAILED delivery was refused as a replay: %s", res.Message)
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer got %d copies, want 1: %v", len(inbox.texts), inbox.texts)
	}
}

// UNCONFIRMED IS NOT HELD FOREVER. When the receiver's records never showed
// the batch, the channel presumes it in flight for the grace and then lets a
// retry through — the failure of over-sealing is a lost escalation.
func TestT428UnconfirmedRetriesAfterGrace(t *testing.T) {
	// Observed but nothing found: the honest indeterminate reading.
	s, inbox := t428Server(t, TurnEvidence{Observed: true})

	now := time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC)
	s.SetNotifyReplayClock(func() time.Time { return now })

	if _, err := s.deliverByName("jevons", t428Batch, OriginAgent, false); err != nil {
		t.Fatalf("first delivery: %v", err)
	}

	now = now.Add(notifyReplayUnconfirmedGrace / 2)
	res, err := s.deliverByName("jevons", t428Batch, OriginAgent, false)
	if err != nil {
		t.Fatalf("within grace: %v", err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("within grace status=%q want suppressed", res.Status)
	}

	now = now.Add(notifyReplayUnconfirmedGrace)
	res, err = s.deliverByName("jevons", t428Batch, OriginAgent, false)
	if err != nil {
		t.Fatalf("past grace: %v", err)
	}
	if res.Status == StatusSuppressedReplay {
		t.Fatalf("past the grace an unconfirmed batch stayed suppressed: %s", res.Message)
	}
	if len(inbox.texts) != 2 {
		t.Fatalf("overseer got %d copies, want 2: %v", len(inbox.texts), inbox.texts)
	}
}

// A SEALED BATCH HAS NO EXPIRY. A timer is the "you are being shown this
// again" generator; only eviction may resurrect a confirmed batch.
func TestT428ConfirmedBatchNeverExpires(t *testing.T) {
	s, _ := t428Server(t, TurnEvidence{Observed: true, PayloadEnteredTurn: true})

	now := time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC)
	s.SetNotifyReplayClock(func() time.Time { return now })

	if _, err := s.deliverByName("jevons", t428Batch, OriginAgent, false); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	now = now.Add(72 * time.Hour)
	res, err := s.deliverByName("jevons", t428Batch, OriginAgent, false)
	if err != nil {
		t.Fatalf("three days later: %v", err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("a confirmed batch expired on a clock: status=%q", res.Status)
	}
}

// THE LEDGER IS BOUNDED, and eviction fails in the safe direction: an old
// batch becomes deliverable again rather than the channel growing forever.
func TestT428LedgerIsBoundedAndEvictsOldest(t *testing.T) {
	l := newNotifyReplayLedger(3, nil)

	for i := range 3 {
		tk, dec := l.Offer(fmt.Sprintf("batch %d", i))
		if !dec.Admit {
			t.Fatalf("batch %d refused on first offer", i)
		}
		tk.Settle(true)
	}
	if _, dec := l.Offer("batch 0"); dec.Admit {
		t.Fatal("batch 0 should still be remembered")
	}

	tk, dec := l.Offer("batch 3")
	if !dec.Admit {
		t.Fatal("a new batch was refused")
	}
	tk.Settle(true)

	// batch 0 was least-recently-offered before "batch 0" was re-offered above,
	// so the eviction victim is batch 1.
	if _, dec := l.Offer("batch 1"); !dec.Admit {
		t.Fatal("the oldest batch was not evicted; the ledger is unbounded")
	}
	if len(l.entries) > 3 {
		t.Fatalf("ledger holds %d entries over a limit of 3", len(l.entries))
	}
}

// The suppression notice is what an operator reads instead of a delivery, so
// it has to say how many times and how long — one stuck source and a whole
// stuck fleet look identical without the count.
func TestT428SuppressionNoticeNamesCountAndAge(t *testing.T) {
	s, _ := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	now := time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC)
	s.SetNotifyReplayClock(func() time.Time { return now })

	if _, err := s.deliverByName("jevons", t428Batch, OriginAgent, false); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	now = now.Add(90 * time.Second)
	res, err := s.deliverByName("jevons", t428Batch, OriginAgent, false)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	for _, want := range []string{"offer #2", "1m30s ago", "1 copy/copies"} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("notice %q missing %q", res.Message, want)
		}
	}
}

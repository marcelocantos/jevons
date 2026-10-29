// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T1252: three messages queued behind a busy seat used to drain one per
// terminal stop — the seat's own long turn would have to end again, and
// again, before the backlog finished. They must drain together as ONE turn
// at the stop that frees the seat, when that one turn confirms delivered.
func TestT1252QueuedBacklogDrainsAsOneTurnOnTerminalStop(t *testing.T) {
	f := newBindFixture(t, "T1252")
	for _, payload := range []string{
		"owner note 1: land the fix",
		"owner note 2: also check the gate",
		"owner note 3: ping me when done",
	} {
		if _, _, err := f.s.sendQueue().Append(f.name, payload, time.Now()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if depth := observedPendingSends(f.s, f.name); depth != 3 {
		t.Fatalf("pending depth = %d; want 3 queued ahead of the drain", depth)
	}

	receiptJournal(t, f.s, t.TempDir())
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		return heldReceipt(q, claudiawire.ReceiptAccepted), nil
	})

	// The terminal stop: one drain call delivers the whole backlog as ONE
	// bound handoff.
	f.s.drainAgentSendQueue(f.name)

	sent := f.probe.sentIDs()
	if len(sent) != 1 {
		t.Fatalf("handed off %d separate turns; want exactly 1 turn carrying all 3 messages (ids=%v)", len(sent), sent)
	}

	merged := f.probe.sent[0]
	for i, want := range []string{"owner note 1", "owner note 2", "owner note 3"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("message %d (%q) missing from the merged turn: %q", i, want, merged)
		}
	}
	if strings.Index(merged, "owner note 1") > strings.Index(merged, "owner note 2") ||
		strings.Index(merged, "owner note 2") > strings.Index(merged, "owner note 3") {
		t.Fatalf("messages not delivered in order: %q", merged)
	}

	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("accepted receipt should have cleared the merged backlog: %+v %v", entries, err)
	}
	if depth := observedPendingSends(f.s, f.name); depth != 0 {
		t.Fatalf("pending depth after drain = %d; want 0 — nothing left for a second terminal stop to deliver", depth)
	}
}

// 🎯T1252: a merge that fails to confirm must not cost the per-entry retry
// guarantees ClaimFront already gives (🎯T418 / 🎯T623). An Unverified
// outcome holds only the OLDEST message (exactly as a lone ClaimFront claim
// would); the rest stay plain Pending for the next drain to pick up — never
// merged away, never silently lost.
func TestT1252FailedMergeUnpacksInsteadOfHoldingTheWholeBacklog(t *testing.T) {
	dir := t.TempDir()
	const name = "jv-worker"
	s, _, _ := t418Daemon(t, dir)
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return nil, false, nil })

	for _, payload := range []string{"first", "second", "third"} {
		if _, _, err := s.sendQueue().Append(name, payload, time.Now()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}

	s.drainAgentSendQueue(name) // no live process: DefinitelyNotSent on the head

	entries, err := s.sendQueue().Snapshot(name)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("unpacked backlog lost or merged entries: %+v", entries)
	}
	if entries[0].Text != "first" || entries[0].State != sendq.Pending {
		t.Fatalf("head entry not retried as a plain pending retry: %+v", entries[0])
	}
	for i, want := range []string{"second", "third"} {
		if entries[i+1].Text != want {
			t.Fatalf("member %d = %+v; want text %q untouched", i+1, entries[i+1], want)
		}
	}
}


// 🎯T1252 / 🎯T419: the unpack-on-failure path (ResolveClaimAllFailure) must
// be exercised through its REAL caller — drainAgentSendQueueOnce — with a
// receipt-capable fixture, not only in isolation (sendq package) or through
// an incapable sender that never reaches ClaimAll at all (the test above
// this one uses t418Daemon's plain recordingSender, which has no
// SendWithRequestID and so is gated OUT of ClaimAll before it ever claims).
// Here the process IS capable (newBindFixture's bindProbe), ClaimAll DOES
// merge the backlog, and the receipt query returns a non-Confirmed state
// (ReceiptUnknown) for the merged send — the one shape that drives
// ResolveClaimAllFailure from drainAgentSendQueueOnce's own resolve closure.
func TestT1252CapableSenderNonConfirmedReceiptUnpacksThroughRealDrain(t *testing.T) {
	f := newBindFixture(t, "T1252")
	receiptJournal(t, f.s, t.TempDir())

	for _, payload := range []string{"first accepted", "second queued", "third queued"} {
		if _, _, err := f.s.sendQueue().Append(f.name, payload, time.Now()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if depth := observedPendingSends(f.s, f.name); depth != 3 {
		t.Fatalf("pending depth = %d; want 3 queued ahead of the drain", depth)
	}

	// A receipt-capable process whose receipt query never settles — the
	// transport obligation does not confirm. drainAgentSendQueueOnce must
	// still have claimed the backlog through ClaimAll (not ClaimFront): the
	// single physical send below carries all three messages.
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		return heldReceipt(q, claudiawire.ReceiptUnknown), nil
	})

	// The real entry point for a terminal-stop drain, exactly as production
	// calls it — not sendq.Store.ResolveClaimAllFailure in isolation.
	f.s.drainAgentSendQueue(f.name)

	sent := f.probe.sentIDs()
	if len(sent) != 1 {
		t.Fatalf("handed off %d separate turns; want exactly 1 merged turn (ids=%v) — ClaimAll did not fire", len(sent), sent)
	}
	merged := f.probe.sent[0]
	for _, want := range []string{"first accepted", "second queued", "third queued"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged turn missing %q: %q", want, merged)
		}
	}

	// ResolveClaimAllFailure's unpack: the OLDEST message takes the failure
	// outcome (held, Uncertain — never resent), and the others return to
	// plain Pending, untouched, exactly as ClaimFront alone would have left
	// them. Nothing stays merged as one blob (that is ClaimDigest's
	// above-threshold shape, not this one), and nothing is lost.
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("unpack lost or re-merged entries: %+v", entries)
	}
	if entries[0].Text != "first accepted" || entries[0].State != sendq.Uncertain {
		t.Fatalf("head entry not held as the failed attempt: %+v", entries[0])
	}
	for i, want := range []string{"second queued", "third queued"} {
		e := entries[i+1]
		if e.Text != want || e.State != sendq.Pending || e.AttemptID != "" {
			t.Fatalf("member %d = %+v; want plain untouched pending %q", i+1, e, want)
		}
	}

	// The unheld tail still drains on the next terminal stop, without
	// resending the held head.
	f.s.drainAgentSendQueue(f.name)
	if got := f.probe.sentIDs(); len(got) != 2 {
		t.Fatalf("second drain sent %d turns; want exactly 2 total (head not resent)", len(got))
	}
	if f.probe.sent[1] == merged || strings.Contains(f.probe.sent[1], "first accepted") {
		t.Fatalf("held head was resent on the next drain: %q", f.probe.sent[1])
	}
}

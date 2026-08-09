// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package wakebatch

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func idle(recipient, worker string, at time.Time) Event {
	return Event{Recipient: recipient, Kind: "worker-idle", Subject: worker,
		Detail: worker + " is idle with an open mission", At: at}
}

// The whole point: four idle workers under one PO cost one wake, not four.
func TestFourEventsBecomeOneDigest(t *testing.T) {
	b := New(3 * time.Minute)
	for i, w := range []string{"jv-a", "jv-b", "jv-c", "jv-d"} {
		if now := b.Add(idle("jevons-po", w, t0.Add(time.Duration(i)*time.Second))); now {
			t.Fatalf("%s asked to deliver immediately", w)
		}
	}
	if got := b.Due(t0.Add(2 * time.Minute)); len(got) != 0 {
		t.Fatalf("due before the window elapsed: %v", got)
	}
	due := b.Due(t0.Add(3 * time.Minute))
	if len(due) != 1 || due[0] != "jevons-po" {
		t.Fatalf("due=%v want [jevons-po]", due)
	}
	evs := b.Take("jevons-po")
	if len(evs) != 4 {
		t.Fatalf("took %d events want 4", len(evs))
	}
	digest := FormatDigest(evs)
	for _, w := range []string{"jv-a", "jv-b", "jv-c", "jv-d"} {
		if !strings.Contains(digest, w) {
			t.Errorf("digest dropped %s:\n%s", w, digest)
		}
	}
	if b.Pending("jevons-po") != 0 {
		t.Error("Take left events behind; they would be delivered twice")
	}
}

// A worker that flaps idle repeatedly is still one idle worker.
func TestRepeatsCollapse(t *testing.T) {
	b := New(time.Minute)
	for i := 0; i < 5; i++ {
		b.Add(idle("jevons-po", "jv-a", t0.Add(time.Duration(i)*time.Second)))
	}
	if n := b.Pending("jevons-po"); n != 1 {
		t.Fatalf("pending=%d want 1", n)
	}
}

// Each recipient batches independently: a busy PO must not delay the
// overseer's digest or vice versa.
func TestRecipientsAreIndependent(t *testing.T) {
	b := New(time.Minute)
	b.Add(idle("jevons-po", "jv-a", t0))
	b.Add(idle("jevons", "og-b", t0.Add(90*time.Second)))
	// Each recipient's window starts at its own first event, so at t0+2m
	// the PO is due and the overseer (first event at t0+90s) is not.
	if due := b.Due(t0.Add(2 * time.Minute)); len(due) != 1 || due[0] != "jevons-po" {
		t.Fatalf("due=%v want [jevons-po] only", due)
	}
	due := b.Due(t0.Add(150 * time.Second))
	if len(due) != 2 {
		t.Fatalf("due=%v want both", due)
	}
	b.Take("jevons-po")
	if b.Pending("jevons") != 1 {
		t.Fatal("taking one recipient's digest disturbed another's")
	}
}

// Batching off must behave exactly as before: deliver immediately.
func TestZeroWindowDeliversImmediately(t *testing.T) {
	b := New(0)
	if now := b.Add(idle("jevons-po", "jv-a", t0)); !now {
		t.Fatal("zero window must report deliver-now")
	}
	if b.Pending("jevons-po") != 0 {
		t.Fatal("zero window queued an event that will never flush")
	}
	var nilB *Batcher
	if now := nilB.Add(idle("x", "y", t0)); !now {
		t.Fatal("nil batcher must report deliver-now rather than swallow the event")
	}
}

// One event renders as itself: digest ceremony around a single line would
// make the common case more expensive than what it replaced.
func TestSingleEventRendersPlain(t *testing.T) {
	got := FormatDigest([]Event{idle("jevons-po", "jv-a", t0)})
	if strings.Contains(got, "digest:") {
		t.Fatalf("single event wrapped in digest ceremony: %q", got)
	}
	if got != "jv-a is idle with an open mission" {
		t.Fatalf("got %q", got)
	}
}

func TestEmptyDigestIsEmpty(t *testing.T) {
	if got := FormatDigest(nil); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

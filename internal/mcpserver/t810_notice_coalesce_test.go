// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// 🎯T810 — a fleet-health notice whose material content is unchanged does not
// start another overseer turn, and the turns saved are counted per key.

func t810Stalled(depth int, age string) string {
	return fmt.Sprintf("Stalled backlog on \"cl-t108-readiness-at-load\": %d message(s) held by the daemon, "+
		"the oldest waiting %s, because it has no live process to deliver to.", depth, age)
}

func TestT810RepeatedStalledBacklogIsOneTurnAndCountsSaved(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	now := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	s.SetNoticeCoalesceClock(func() time.Time { return now })

	const n = 6
	for i := range n {
		// Age advances every sweep: byte-different, materially identical.
		s.notifyFleetHealth("b1bec7a0e257", t810Stalled(1, fmt.Sprintf("2h%dm%ds", 29+i/2, 36+i)))
		now = now.Add(40 * time.Second)
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer turns=%d want 1 for %d materially identical notices\n%v", len(inbox.texts), n, inbox.texts)
	}
	if got := s.NoticeSavedTurns()["b1bec7a0e257"]; got != n-1 {
		t.Fatalf("saved=%d want %d", got, n-1)
	}
	if line := s.FormatNoticeSavings(); !strings.Contains(line, "b1bec7a0e257") || !strings.Contains(line, "5") {
		t.Fatalf("agent_list savings line missing key/count: %q", line)
	}
}

func TestT810MaterialChangeDeliversAgain(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	now := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	s.SetNoticeCoalesceClock(func() time.Time { return now })

	s.notifyFleetHealth("k1", t810Stalled(1, "20m0s"))
	s.notifyFleetHealth("k1", t810Stalled(1, "21m0s"))  // same age bucket: saved
	s.notifyFleetHealth("k1", t810Stalled(2, "22m0s"))  // count changed: new turn
	s.notifyFleetHealth("k1", t810Stalled(2, "7h1m0s")) // age crossed a threshold: new turn
	if len(inbox.texts) != 3 {
		t.Fatalf("overseer turns=%d want 3\n%v", len(inbox.texts), inbox.texts)
	}
	if got := s.NoticeSavedTurns()["k1"]; got != 1 {
		t.Fatalf("saved=%d want 1", got)
	}
}

func TestT810StateClearingRedelivers(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	now := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	s.SetNoticeCoalesceClock(func() time.Time { return now })

	s.notifyFleetHealth("k1", t810Stalled(1, "20m0s"))
	now = now.Add(NoticeClearedAfter + time.Minute) // no offers: the condition cleared
	s.notifyFleetHealth("k1", t810Stalled(1, "21m0s"))
	if len(inbox.texts) != 2 {
		t.Fatalf("overseer turns=%d want 2 after the state cleared and recurred\n%v", len(inbox.texts), inbox.texts)
	}
}

func TestT810DistinctOccurrencesAreIndependent(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	s.notifyFleetHealth("k1", t810Stalled(1, "20m0s"))
	s.notifyFleetHealth("k2", t810Stalled(1, "20m0s"))
	if len(inbox.texts) != 2 {
		t.Fatalf("overseer turns=%d want 2", len(inbox.texts))
	}
}

func TestT810SavedCountSurvivesBounce(t *testing.T) {
	dir := t.TempDir()
	first := &overseerInbox{}
	s1 := t568Server(t, dir, first)
	s1.notifyFleetHealth("k1", t810Stalled(1, "20m0s"))
	s1.notifyFleetHealth("k1", t810Stalled(1, "21m0s"))

	second := &overseerInbox{}
	s2 := t568Server(t, dir, second)
	s2.notifyFleetHealth("k1", t810Stalled(1, "22m0s"))
	if len(first.texts) != 1 || len(second.texts) != 0 {
		t.Fatalf("turns before=%d after bounce=%d want 1 and 0", len(first.texts), len(second.texts))
	}
	if got := s2.NoticeSavedTurns()["k1"]; got != 2 {
		t.Fatalf("saved after bounce=%d want 2", got)
	}
}

// 🎯T810 (7): replay the cl-t108-readiness-at-load stalled-backlog sequence
// captured from jevonsd.log (2026-09-22 02:19–04:38): 72 notices, ages 11m30s
// to 2h44m. Delivered notices are the first, the 15m crossing and the 1h
// crossing; every other sweep is saved.
func TestT810ReplayRealClT108Sequence(t *testing.T) {
	raw, err := os.ReadFile("testdata/t810_cl_t108_stalled.tsv")
	if err != nil {
		t.Fatal(err)
	}
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	var now time.Time
	s.SetNoticeCoalesceClock(func() time.Time { return now })
	rows := 0
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		f := strings.Split(l, "\t")
		at, err := time.Parse("2006-01-02T15:04:05.000-07:00", f[0])
		if err != nil {
			t.Fatalf("row %q: %v", l, err)
		}
		now = at
		age := strings.TrimPrefix(f[2], "oldest_age=")
		s.notifyFleetHealth("b1bec7a0e257", fmt.Sprintf(
			"Stalled backlog on %q: 1 message(s) held by the daemon, the oldest waiting %s, because it has no live process to deliver to.",
			"cl-t108-readiness-at-load", age))
		rows++
	}
	const wantDelivered = 3
	if rows != 72 || len(inbox.texts) != wantDelivered {
		t.Fatalf("rows=%d delivered=%d want 72 and %d", rows, len(inbox.texts), wantDelivered)
	}
	if got := s.NoticeSavedTurns()["b1bec7a0e257"]; got != rows-wantDelivered {
		t.Fatalf("saved=%d want %d", got, rows-wantDelivered)
	}
}

// Owner-origin sends never pass through the coalescer.
func TestT810OwnerSendsAreNeverCoalesced(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	for range 3 {
		if _, err := s.deliverByName("jevons", "same owner words", OriginOwner, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(inbox.texts) != 3 {
		t.Fatalf("owner sends delivered=%d want 3", len(inbox.texts))
	}
	if len(s.NoticeSavedTurns()) != 0 {
		t.Fatal("owner sends touched the notice coalescer")
	}
}

// 🎯T810 (PO check): an unconfirmed first delivery does not suppress repeats.
// Repeats are re-offered, uncounted, until one is confirmed; saved counts start
// from confirmation. Past the bounded window an unconfirmed copy counts as landed.
func TestT810UnconfirmedFirstDeliveryIsReofferedUntilConfirmed(t *testing.T) {
	ev := TurnEvidence{}
	s := &Server{}
	inbox := &overseerInbox{}
	s.SetOverseerDeliver(inbox.deliver)
	s.SetTurnWitness(func(string, string) turnWatch { return func() TurnEvidence { return ev } })
	now := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	s.SetNoticeCoalesceClock(func() time.Time { return now })
	s.SetNotifyReplayClock(func() time.Time { return now })

	// First delivery and three repeats, none confirmed: each is re-offered.
	for i := range 4 {
		s.notifyFleetHealth("k1", t810Stalled(1, fmt.Sprintf("2%d m", i)))
		now = now.Add(40 * time.Second)
	}
	if got := s.NoticeSavedTurns()["k1"]; got != 0 {
		t.Fatalf("saved=%d while unconfirmed; want 0 (repeats must be re-offered)", got)
	}
	if len(inbox.texts) < 4 {
		t.Fatalf("delivered=%d want 4 re-offers while unconfirmed", len(inbox.texts))
	}

	// One copy is confirmed; later repeats are saved from there.
	ev = TurnEvidence{Observed: true, PayloadSeen: true}
	s.notifyFleetHealth("k1", t810Stalled(1, "26m0s"))
	before := len(inbox.texts)
	s.notifyFleetHealth("k1", t810Stalled(1, "27m0s"))
	s.notifyFleetHealth("k1", t810Stalled(1, "28m0s"))
	if len(inbox.texts) != before {
		t.Fatalf("repeats after confirmation reached the overseer: %d -> %d", before, len(inbox.texts))
	}
	if got := s.NoticeSavedTurns()["k1"]; got != 2 {
		t.Fatalf("saved=%d want 2 counted from confirmation", got)
	}
}

func TestT810UnconfirmedCountsAsLandedAfterBoundedWindow(t *testing.T) {
	s := &Server{}
	inbox := &overseerInbox{}
	s.SetOverseerDeliver(inbox.deliver)
	s.SetTurnWitness(witnessYielding(TurnEvidence{}))
	now := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	s.SetNoticeCoalesceClock(func() time.Time { return now })
	s.SetNotifyReplayClock(func() time.Time { return now })

	s.notifyFleetHealth("k1", t810Stalled(1, "20m0s"))
	now = now.Add(5 * time.Minute) // inside the window: re-offered, not saved
	s.notifyFleetHealth("k1", t810Stalled(1, "21m0s"))
	if got := s.NoticeSavedTurns()["k1"]; got != 0 {
		t.Fatalf("saved=%d inside the re-offer window; want 0", got)
	}
	now = now.Add(notifyReplayUnconfirmedGrace) // past the window measured from the first copy
	before := len(inbox.texts)
	s.notifyFleetHealth("k1", t810Stalled(1, "22m0s"))
	if len(inbox.texts) != before || s.NoticeSavedTurns()["k1"] != 1 {
		t.Fatalf("past the bounded window the copy must count as landed: delivered %d->%d saved=%d",
			before, len(inbox.texts), s.NoticeSavedTurns()["k1"])
	}
}

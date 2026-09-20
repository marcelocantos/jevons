// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T706 — the incident 🎯T582 did not cover.
//
// 🎯T582 put a once-per-seat seam on the ROUTED path: a backlog held against a
// seat reaped by reap_achieve / reap_done is drained to the parent and
// announced once. Every other reap — an explicit product:kill, a
// stop_engagement, a dead_seat sweep — is not routable (the seat can genuinely
// be started again to read its queue), so it fell through to the 🎯T401
// hold-and-report branch, which notified unconditionally on a thirty-second
// timer. jv-t679.1-evidence-seam alarmed every ~30s for three hours after an
// explicit kill.
//
// The clock is injected (the 🎯T582 fixture), so three hours of sweeps is
// arithmetic rather than a sleep.

// The acceptance clause: a killed seat's held backlog surfaces once, not every
// sweep, and the single notice still carries the recovery call.
func TestT706KilledSeatHeldBacklogAlertsOnce(t *testing.T) {
	const agent, parent = "jv-t679.1-evidence-seam", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonKill)

	if _, _, err := f.s.sendQueue().Append(agent,
		"follow-up for the evidence seam; please re-read the gate line",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}

	// The specimen's own duration: three hours of the daemon's idle cadence.
	f.sweepFor(3 * time.Hour)

	lines := f.up.all()
	got := heldReapedNotices(lines)
	if got != 1 {
		t.Fatalf("overseer notices = %d over 3 simulated hours of 30s sweeps; want exactly 1\n%s",
			got, strings.Join(lines, "\n"))
	}
	one := ""
	for _, l := range lines {
		if strings.Contains(l, "Held backlog on") {
			one = l
		}
	}
	// A killed seat IS recoverable, unlike 🎯T582's finished one: the single
	// notice must still say how, or suppression has cost the operator the fix.
	if !strings.Contains(one, "jevons_agent_start") {
		t.Fatalf("the one notice dropped the recovery hint:\n%s", one)
	}
	if !strings.Contains(one, "🎯T706") {
		t.Fatalf("the one notice does not say it is said once:\n%s", one)
	}

	// Suppression is not deletion: the hold is still on disk for the seat to
	// drain when it comes back.
	backlogs, err := f.s.sendQueue().Backlogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(backlogs) != 1 || backlogs[0].Depth != 1 {
		t.Fatalf("held backlog after suppression = %+v; want the message still held", backlogs)
	}
}

// The blindspot a bare per-seat flag leaves: forgetReapedBacklogNotice only
// runs on a sweep that sees the name registered AND still holding, so a seat
// that is reaped, drained, restarted and reaped again would never announce its
// second, genuinely new backlog. The seam keys on the hold, not the name.
func TestT706NewHoldOnTheSameNameAnnouncesAgain(t *testing.T) {
	const agent, parent = "jv-t679.1-evidence-seam", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonKill)
	q := f.s.sendQueue()

	if _, _, err := q.Append(agent, "first hold",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.sweepFor(30 * time.Minute)
	if got := heldReapedNotices(f.up.all()); got != 1 {
		t.Fatalf("first hold notices = %d; want 1", got)
	}

	// The seat came back, drained its queue, finished, and was killed again.
	first, err := q.Backlogs()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range first[0].EntryIDs {
		e, claimed, err := q.ClaimFrontID(agent, id)
		if err != nil || !claimed {
			t.Fatalf("claim %s: claimed=%v err=%v", id, claimed, err)
		}
		if err := q.Resolve(agent, e, sendq.Confirmed, "drained by the restarted seat"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := q.Append(agent, "second, unrelated hold",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}

	f.sweepFor(30 * time.Minute)
	if got := heldReapedNotices(f.up.all()); got != 2 {
		t.Fatalf("notices after a second, distinct hold = %d; want 2 (one per hold)\n%s",
			got, strings.Join(f.up.all(), "\n"))
	}
}

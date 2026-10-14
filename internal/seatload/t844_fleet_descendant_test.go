// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatload

import (
	"syscall"
	"testing"
	"time"
)

// 🎯T844 specimen: seat A (900) ran the daily activation bounce from its own
// turn. The script it ran (901) is A's own process; the relaunched daemon
// (600) and the broker (601) came up as 901's children, and seat B (700) was
// minted under the relaunched daemon, so B's own process (700) and its child
// (701) are genuine process-tree descendants of A's root. A pure parent-link
// walk from A's anchor correctly calls all of 901, 600, 601, 700, 701
// descendants of A — the walk is not wrong, the selection is: none of
// 600 (self), 601 (the broker) or 700/701 (seat B, a different registered
// seat) may ever be reaped as a side effect of reaping A.
func fleetBounceTable() Table {
	return Table{
		{PID: 1, PPID: 0, PGID: 1, Command: "/sbin/launchd"},
		{PID: 900, PPID: 1, PGID: 900, Command: "claude --seat jv-t814-sentinel-coalesce"},
		{PID: 901, PPID: 900, PGID: 900, Command: "/bin/sh scripts/restart-daily-jevonsd.sh"},
		{PID: 600, PPID: 901, PGID: 600, Command: "jevonsd"},
		{PID: 601, PPID: 901, PGID: 601, Command: "claudia broker"},
		{PID: 700, PPID: 600, PGID: 700, Command: "claude --seat yourworld2-po"},
		{PID: 701, PPID: 700, PGID: 700, Command: "yourworld2-po child"},
		// A's own detached load, reparented to init but still in A's group
		// (900) — the 🎯T708 shape this fix must not regress.
		{PID: 950, PPID: 1, PGID: 900, Command: "while :; do sleep 1; done"},
	}
}

func TestT844ReapOfBounceSeatNeverSignalsAnotherRegisteredSeat(t *testing.T) {
	tbl := fleetBounceTable()
	a, err := AnchorFor(tbl, "jv-t814-sentinel-coalesce", 900, time.Now())
	if err != nil {
		t.Fatalf("AnchorFor A: %v", err)
	}
	b, err := AnchorFor(tbl, "yourworld2-po", 700, time.Now())
	if err != nil {
		t.Fatalf("AnchorFor B: %v", err)
	}

	// Sanity: the naive parent-link walk from A really does reach B and the
	// broker — that is the defect's precondition, not a quirk of the test.
	naive := map[int]bool{}
	for _, p := range Descendants(tbl, a) {
		naive[p.PID] = true
	}
	for _, pid := range []int{600, 601, 700, 701} {
		if !naive[pid] {
			t.Fatalf("precondition failed: pid %d is not even a naive descendant of A; the specimen does not reproduce the shape", pid)
		}
	}

	var signalled []int
	tr := &Tracker{
		List:   func() (Table, error) { return tbl, nil },
		Signal: func(pid int, _ syscall.Signal) error { signalled = append(signalled, pid); return nil },
		Sleep:  func(time.Duration) {},
		Self:   600,
	}
	// The daemon anchors every live seat before reaping any one of them —
	// exactly what TrackSeatLoad does each sweep.
	tr.mu.Lock()
	tr.anchors = map[string]Anchor{a.Seat: a, b.Seat: b}
	tr.mu.Unlock()

	res, err := tr.ReapAnchor(a)
	if err != nil {
		t.Fatalf("ReapAnchor: %v", err)
	}
	for _, pid := range signalled {
		if pid == 600 {
			t.Fatalf("reaping A signalled the daemon itself (pid %d)", pid)
		}
		if pid == 700 || pid == 701 {
			t.Fatalf("reaping A signalled registered seat B's process (pid %d) — 🎯T844", pid)
		}
	}
	// NOT asserted here: pid 601 (the broker). The broker is not a jevons
	// registry seat, so TrackSeatLoad never anchors it and this fix has no
	// fact to exclude it by. Closing that half of the acceptance needs the
	// broker's own pid (or socket-peer credential) surfaced through a public
	// claudia API — claudia's broker socket path and pid helpers are
	// unexported (internal/broker.SocketPath, broker_mode.brokerSocketPath)
	// and jevons cannot reach them without a claudia-side change. Filed as
	// residual risk in the finish report rather than worked around with a
	// fragile ps-name match on "claudia ... broker".
	// A's own detached load (950, the 🎯T708 shape) is A's legitimate
	// descendant and must still be reaped — this fix must not become a
	// blanket refusal to reap anything.
	foundOwn := false
	for _, pid := range signalled {
		if pid == 950 {
			foundOwn = true
		}
	}
	if !foundOwn {
		t.Fatalf("reap of A signalled nothing of A's own (950): %+v / %s", signalled, res)
	}
}

// Without the other-anchor exclusion the naive selection reaches the
// broker and seat B; this test pins that regression directly against
// ReapAnchor with only A tracked (no sibling anchors recorded), which is
// the pre-🎯T844 shape, to document exactly what the fix changed.
func TestT844WithoutSiblingAnchorTheNaiveSelectionWouldReachThem(t *testing.T) {
	tbl := fleetBounceTable()
	a, _ := AnchorFor(tbl, "jv-t814-sentinel-coalesce", 900, time.Now())

	var signalled []int
	tr := &Tracker{
		List:   func() (Table, error) { return tbl, nil },
		Signal: func(pid int, _ syscall.Signal) error { signalled = append(signalled, pid); return nil },
		Sleep:  func(time.Duration) {},
		Self:   600,
		// No other anchors recorded: the daemon never anchored seat B or
		// the broker (the broker is not a registered seat at all). This is
		// why the fix cannot be "trust the other-anchors set alone" for the
		// broker — see the finish report's residual-risk note.
	}
	if _, err := tr.ReapAnchor(a); err != nil {
		t.Fatalf("ReapAnchor: %v", err)
	}
	gotB := false
	for _, pid := range signalled {
		if pid == 700 || pid == 701 {
			gotB = true
		}
	}
	if !gotB {
		t.Fatalf("expected the unanchored-sibling case to still reach seat B's pids (documenting the residual gap when a seat has not yet been anchored), signalled=%v", signalled)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T1066: the faithful shape of the 2026-10-11 wake wave.
//
// The incident was NOT a dead seat relaunched under AutoStart (the shape a
// prior attempt on this target tested and fixed, on
// jevons-worktree/jv-t1066-idle-admission — that fix is real but orthogonal:
// it guards ensureAgentProcess, which only runs for a seat the registry does
// not already see alive). The actual broker-owned seats (jv-t1020-auto,
// jv-t1008-auto) were ALREADY RUNNING when their idle-nudge briefs were
// admitted into the daemon's own sendq, while fleet intent still read
// Working. blocked_owner landed 5-20 minutes later, after admission but
// before the seat's live turn ended and the queue drained — and nothing on
// the drain path re-read intent at that later hand-off. ensureAgentProcess's
// ControlDeliverStart gate (🎯T408) never engages for a seat already alive:
// ensureAgentProcess returns the live process in its second branch, above
// every not-running check, so the control that would have refused the start
// never runs for a live seat. Fixing ensureAgentProcess alone, as the
// idle-admission branch set out to, leaves this race wide open.
//
// This test reproduces the race directly on drainAgentSendQueueOnce — the
// one place a daemon-held queue entry is actually handed to the process —
// with the same ordering the incident had: admit while Working, flip intent,
// then attempt the drain. It asserts no model turn occurs (the recording
// sender never sees the text) and that the message survives, held for a
// retry once intent clears, rather than being lost or silently re-ordered.
func TestT1066QueuedPayloadNotDeliveredAfterBlockedOwnerLands(t *testing.T) {
	const name = "jv-t1020-auto"
	const payload = "idle-nudge: continue the mission"

	dir := t.TempDir()
	s, sender, _ := t418Daemon(t, dir)

	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)

	// Admission: the brief is accepted into the daemon's own queue while
	// intent is Working — exactly what the idle-nudge sweep's own
	// ControlNotifyIdle check permits at the moment it sends.
	if dec := s.AllowFleetControl(name, fleetintent.ControlNotifyIdle); !dec.Allow {
		t.Fatalf("precondition: intent must read Working at admission, got %+v", dec)
	}
	entry, _, err := s.sendQueue().Append(name, payload, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// The seat is alive and mid-turn-boundary when the owner's stabilization
	// pause lands — the fleet-wide blocked_owner intent the 00:10:30Z brief
	// restores after this turn. Admission already happened; this is the
	// 5-20 minute gap the incident lived in.
	if err := s.SetFleetIntent(fleetintent.BlockedOwner, "owner", "stabilization pause"); err != nil {
		t.Fatal(err)
	}

	// The drain fires — a turn boundary, a sweep, whatever triggers it. The
	// seat is reachable (sender resolves, seat is observed alive): the old
	// ensureAgentProcess-only gate has nothing to refuse here because this
	// path never calls ensureAgentProcess at all.
	s.drainAgentSendQueue(name)

	if got := sender.delivered(); len(got) != 0 {
		t.Fatalf("model turn began under blocked_owner intent: delivered=%v", got)
	}

	entries, err := s.sendQueue().Snapshot(name)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID || entries[0].Text != payload {
		t.Fatalf("payload lost or reordered instead of held: %+v", entries)
	}
	if entries[0].State != sendq.Pending {
		t.Fatalf("held entry must stay Pending for retry, got state=%v", entries[0].State)
	}

	// Lifting the intent (the owner's own clearance, 🎯T414's only licence)
	// lets the very same entry drain on the next sweep — the hold is a
	// delay, not a drop.
	if err := s.SetFleetIntent(fleetintent.Working, "owner", "stabilization complete"); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue(name)
	if got := sender.delivered(); len(got) != 1 || got[0] != payload {
		t.Fatalf("payload did not drain once intent cleared: delivered=%v", got)
	}
}

// Per-agent blocked_owner (not just the fleet-wide wall) must hold the same
// drain: a worker individually marked blocked_owner is the 🎯T413 "needs the
// owner, pressuring it spends turns on nothing" case, and a live seat must
// not receive a turn either.
func TestT1066QueuedPayloadNotDeliveredWhenAgentBlockedOwner(t *testing.T) {
	const name = "jv-t1008-auto"
	const payload = "idle-nudge: continue the mission"

	dir := t.TempDir()
	s, sender, _ := t418Daemon(t, dir)

	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)

	if _, _, err := s.sendQueue().Append(name, payload, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentIntent(name, fleetintent.BlockedOwner, "owner", "needs a decision"); err != nil {
		t.Fatal(err)
	}

	s.drainAgentSendQueue(name)

	if got := sender.delivered(); len(got) != 0 {
		t.Fatalf("model turn began on a per-agent blocked_owner seat: delivered=%v", got)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Pending {
		t.Fatalf("held entry must stay Pending for retry: %+v %v", entries, err)
	}
}

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
//
// LOG-PROVENANCE BOUNDARY (honest residual, not closed by this test or the
// fix it drives): this suite exercises the race ENTIRELY within jevons's own
// internal/sendq rows — the entry sits in sendq.Pending/Attempting state,
// visible to Snapshot()/Backlogs(), until drainAgentSendQueueOnce (now gated
// by ControlDeliverSubmit) or the immediate trySend() path (now gated the
// same way, see TestT1066ImmediateIdleSubmitHeldByIntent) hands it to
// agentSender.Send(). That Send() call crosses into
// github.com/marcelocantos/claudia's Agent.Send — a tmux-attached or
// broker-RPC process boundary this repository does not own. Once Send()
// returns success, the text is inside Claude Code's own composer / the
// broker's own turn machinery, and NOTHING in jevons can retract it:
// Interrupt() cuts an already-STARTED turn, it does not un-queue a prompt
// the provider's own CLI has accepted but not yet begun.
//
// This matters for reading the real 2026-10-11 07:30-07:46 incident
// evidence, which this worktree does not have access to re-examine: if the
// observed admission timestamp in those logs was a sendq.Append (jevons-side,
// pending in OUR queue) that is the shape this fix closes, proven by the
// red-before/green-after delta on both tests in this file. If instead the
// logs show the payload had already crossed an agentSender.Send() call
// (i.e. was accepted by Claudia/the broker) BEFORE blocked_owner landed, no
// code change on this side of that boundary can retract it — that is a hard
// external limit, not a bug this commit can close, and the only available
// mitigation is a policy one: have blocked_owner's setter also call
// Interrupt() on every seat observed in-flight at the moment the intent
// lands, which cuts the turn the provider already started rather than
// preventing one that was never ours to prevent. That mitigation is NOT
// implemented here (it changes SetFleetIntent's side effects, is a larger
// and riskier slice, and was not in this pass's scope) — named as the
// specific next step if the incident log turns out to show that shape.
//
// Un-audited in this worktree: which shape the actual incident logs show.
// I have not been handed the raw 07:25-07:46 log lines for jv-t1020-auto /
// jv-t1008-auto to classify them against sendq.Append vs agentSender.Send,
// and do not claim to have done so.
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

// TestT1066ImmediateIdleSubmitHeldByIntent is the twin race on the
// SYNCHRONOUS path: a seat that is alive and IDLE right now (no queue
// involved at all) must not receive a direct submit while fleet/agent
// intent says it should not run. This is the gap ControlDeliverStart
// cannot close — ensureAgentProcess only guards launching a STOPPED
// process — and deliverToSenderModeWithRequestID's trySend() is the one
// place left before the text reaches the process.
//
// Held the same way a busy seat is: into the daemon's own sendq, so lifting
// the intent drains the very same message rather than losing it.
func TestT1066ImmediateIdleSubmitHeldByIntent(t *testing.T) {
	const name = "jv-t1020-auto"
	const payload = "idle-nudge: continue the mission"

	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)

	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	if err := s.SetFleetIntent(fleetintent.BlockedOwner, "owner", "stabilization pause"); err != nil {
		t.Fatal(err)
	}

	proc := &fakeSender{alive: true}
	res, err := deliverObservedToSender(s, name, payload, false, proc, false)
	if err != nil {
		t.Fatalf("held send must not error: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q; want queued (held, not delivered)", res.Status)
	}
	if got := proc.sent; len(got) != 0 {
		t.Fatalf("model turn began on an idle seat under blocked_owner intent: sent=%v", got)
	}

	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 || entries[0].Text != payload || entries[0].State != sendq.Pending {
		t.Fatalf("payload not held for retry: %+v %v", entries, err)
	}

	// Lift the intent and drain: the same held message reaches the process.
	if err := s.SetFleetIntent(fleetintent.Working, "owner", "stabilization complete"); err != nil {
		t.Fatal(err)
	}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return proc, false, nil })
	s.drainAgentSendQueue(name)
	if got := proc.sent; len(got) != 1 || got[0] != payload {
		t.Fatalf("held message did not drain once intent cleared: sent=%v", got)
	}
}

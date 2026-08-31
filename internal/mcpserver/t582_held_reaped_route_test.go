// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// 🎯T582 — the incident this suite reproduces: one held message on a seat
// reaped for achieving its target, and a sweep that runs every thirty seconds,
// produced twenty-six identical fleet-health alerts in thirteen minutes, each
// advising a start on a finished seat.
//
// The clock is injected, so a ten-minute run of the sweep is arithmetic rather
// than a sleep.

type t582Fixture struct {
	s      *Server
	up     *upward
	parent *recordingSender
	now    time.Time
}

func t582Server(t *testing.T, agent, parent, reason string) *t582Fixture {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)
	s.SetSendQueueDir(dir)
	s.SetRemovalAccount(fleetlog.New(nil))

	f := &t582Fixture{s: s, up: &upward{}, parent: &recordingSender{}}
	f.now = time.Date(2026, 8, 29, 18, 43, 0, 0, time.UTC)
	s.SetSweepClock(func() time.Time { return f.now })
	s.SetOverseerDeliver(f.up.deliver)
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		if name == parent {
			return f.parent, false, nil
		}
		return nil, false, nil
	})
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})

	// The worker existed, was reaped on finish, and the removal account is
	// what still knows its lineage.
	if err := reg.Register(claudia.AgentDef{
		Name: agent, WorkDir: t.TempDir(), SessionID: "s-" + agent,
		Purpose: claudia.PurposeWork, TargetID: "T577", Parent: parent,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: parent, WorkDir: t.TempDir(), SessionID: "s-" + parent,
		Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	removed, err := s.RemovalAccount().Remove(reg, agent, fleetlog.Removal{
		Reason: reason,
		Detail: "reaped on achieve of 🎯T577",
	})
	if err != nil || !removed {
		t.Fatalf("accounted remove: removed=%v err=%v", removed, err)
	}
	s.MarkAgentReaped(agent, "product:"+reason, "reaped on achieve of 🎯T577")
	if _, ok := LookupReapedRecord(s.fleetIntent(), agent); !ok {
		t.Fatal("reaped intent missing")
	}
	return f
}

// sweepFor runs the sweep every 30s of simulated time for d, the cadence the
// daemon's idle loop uses.
func (f *t582Fixture) sweepFor(d time.Duration) {
	for elapsed := time.Duration(0); elapsed < d; elapsed += 30 * time.Second {
		f.s.SweepSendBacklogs()
		f.now = f.now.Add(30 * time.Second)
	}
}

func heldReapedNotices(lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, "Held backlog on") {
			n++
		}
	}
	return n
}

// The acceptance clause: exactly one overseer notice over ten minutes of
// sweeps, and it does not advise starting the finished seat.
func TestT582OneNoticePerReapedSeatOverTenMinutes(t *testing.T) {
	const agent, parent = "jv-t577-no-checkpoint-reap", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonReapAchieve)

	// The message that produced the incident: post-achieve gate feedback,
	// already older than the reporting threshold when the sweep first sees it.
	if _, _, err := f.s.sendQueue().Append(agent,
		"GATE make-test-go exit=1 SUSPECT id=deadbeef — master is red on your commit",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}

	f.sweepFor(10 * time.Minute)

	lines := f.up.all()
	if got := heldReapedNotices(lines); got != 1 {
		t.Fatalf("overseer notices = %d over 10 simulated minutes; want exactly 1\n%s",
			got, strings.Join(lines, "\n---\n"))
	}
	notice := lines[0]
	if strings.Contains(notice, "Recover with jevons_agent_start") {
		t.Fatalf("notice still advises resurrecting a finished seat:\n%s", notice)
	}
	if !strings.Contains(notice, "do NOT jevons_agent_start") {
		t.Fatalf("notice does not say the seat is finished:\n%s", notice)
	}
	if depth := f.s.pendingAgentSends(agent); depth != 0 {
		t.Fatalf("depth = %d; want 0 — the hold is resolved, not left to re-alarm", depth)
	}
	// Gate feedback about the achieved target is dropped, not routed onward.
	if got := len(f.parent.delivered()); got != 0 {
		t.Fatalf("parent received %d message(s); gate feedback about an achieved target has no actor", got)
	}
}

// A hold that is not gate feedback goes to the parent, tagged with the reaped
// name — the message has a reader, and it is not the overseer.
func TestT582NonGateHoldIsRoutedToTheParent(t *testing.T) {
	const agent, parent = "jv-t577-no-checkpoint-reap", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonReapDone)

	if _, _, err := f.s.sendQueue().Append(agent,
		"owner: the follow-up slice for this mission needs a seat",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}

	f.sweepFor(2 * time.Minute)

	if got := len(f.parent.delivered()); got != 1 {
		t.Fatalf("parent received %d message(s); want 1 routed hold", got)
	}
	sent := f.parent.delivered()[0]
	for _, want := range []string{agent, "routed to you as its parent", "follow-up slice"} {
		if !strings.Contains(sent, want) {
			t.Errorf("routed message missing %q:\n%s", want, sent)
		}
	}
	if depth := f.s.pendingAgentSends(agent); depth != 0 {
		t.Fatalf("depth = %d; want 0 after routing", depth)
	}
	if got := heldReapedNotices(f.up.all()); got != 1 {
		t.Fatalf("overseer notices = %d; want exactly 1", got)
	}
}

// 🎯T401 is not weakened: a genuinely new send to a reaped name is still
// accepted, reported reaped-with-reason, and held. Routing is what the SWEEP
// does with a hold nobody came back for, not what the send path does.
func TestT582ReapedNameStaysAReachableAddress(t *testing.T) {
	const agent, parent = "jv-t577-no-checkpoint-reap", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonReapAchieve)

	res, err := f.s.deliverByName(agent, "new work for this name", OriginAgent, false)
	if err != nil {
		t.Fatalf("send to a reaped name must not be a transport error: %v", err)
	}
	if res.Status != StatusReapedHeld {
		t.Fatalf("status = %q; want %s", res.Status, StatusReapedHeld)
	}
	if res.Queued < 1 || f.s.pendingAgentSends(agent) < 1 {
		t.Fatalf("queued=%d depth=%d; a new message to a reaped name is still held",
			res.Queued, f.s.pendingAgentSends(agent))
	}
	// And it is held, not routed, until it has aged past the threshold.
	f.s.SweepSendBacklogs()
	if depth := f.s.pendingAgentSends(agent); depth != 1 {
		t.Fatalf("depth = %d; want 1 — a fresh hold is not routed out from under a start", depth)
	}
}

// A seat parked or killed for a reason other than finish hygiene keeps the
// 🎯T401 hold-and-report behaviour: it may genuinely be restarted to read its
// backlog, so the message stays where it is.
func TestT582NonFinishReapKeepsTheT401Hold(t *testing.T) {
	const agent, parent = "jv-t577-no-checkpoint-reap", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonRotationDrop)

	if _, _, err := f.s.sendQueue().Append(agent, "gate feedback",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.s.SweepSendBacklogs()

	if depth := f.s.pendingAgentSends(agent); depth != 1 {
		t.Fatalf("depth = %d; want 1 — a non-finish reap still holds (🎯T401)", depth)
	}
	if got := len(f.parent.delivered()); got != 0 {
		t.Fatalf("parent received %d; a non-finish reap does not route", got)
	}
}

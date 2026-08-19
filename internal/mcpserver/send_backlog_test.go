// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T418 clause 7 — the oracle for the durability half, and it is RED against
// the pre-fix tree for the reason the target names: the daemon's queue was a
// map in this process's memory, so a message accepted before a restart could
// not be read by the daemon that came after it.
//
// The fixture is deliberately two Servers over one directory. That is what a
// restart looks like from the queue's side: the process that accepted the
// message never runs the code that delivers it.

// t418Daemon is one daemon process: a queue rooted in dir, a send seam standing
// in for the provider, and a witness that says a drained message became a turn
// (🎯T416's judgement is not what this suite is about).
func t418Daemon(t *testing.T, dir string) (*Server, *recordingSender, *upward) {
	t.Helper()
	s := &Server{}
	s.SetSendQueueDir(dir)
	sender := &recordingSender{}
	s.SetSenderResolver(func(string) (agentSender, bool, error) { return sender, false, nil })
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})
	up := &upward{}
	s.SetOverseerDeliver(up.deliver)
	return s, sender, up
}

// Clause 1 and 4 together: the message the first daemon accepted is delivered
// by the second one. Nothing is asked of the sender, and nobody presses Enter.
func TestQueuedMessageSurvivesTheDaemonThatAcceptedIt(t *testing.T) {
	dir := t.TempDir()
	const name = "jv-worker"
	const payload = "owner: land the oracle before the bounce"

	accepting, _, _ := t418Daemon(t, dir)
	busy := &fakeSender{alive: true, inFlight: true}
	res, err := deliverToSender(accepting, name, payload, false, busy, false)
	if err != nil {
		t.Fatalf("busy send must queue, not fail: %v", err)
	}
	if res.Status != "queued" || res.Queued != 1 {
		t.Fatalf("status=%q queued=%d; want queued/1", res.Status, res.Queued)
	}

	// The daemon dies here. Everything the next one knows, it reads off disk.
	recovering, sender, up := t418Daemon(t, dir)
	if depth := recovering.pendingAgentSends(name); depth != 1 {
		t.Fatalf("recovered depth = %d; want 1 — the restart ate the queue", depth)
	}
	recovering.ReportRecoveredBacklog()
	if !containsLine(up.all(), "Recovered 1 queued message") {
		t.Fatalf("recovery was silent; overseer saw %q", up.all())
	}

	recovering.drainAgentSendQueue(name)
	if got := sender.delivered(); len(got) != 1 || got[0] != payload {
		t.Fatalf("delivered = %v; want the payload the previous daemon accepted", got)
	}
	if depth := recovering.pendingAgentSends(name); depth != 0 {
		t.Fatalf("depth after delivery = %d; want 0", depth)
	}
}

// Clause 9, on the road the restart opens: the turn boundary this queue was
// waiting for was consumed by the bounce, so waiting for the next one waits for
// an event nothing will generate. The sweep is the daemon's own schedule.
func TestSweepDeliversABacklogNoBoundaryWillEverDrain(t *testing.T) {
	dir := t.TempDir()
	const name = "jv-worker"

	accepting, _, _ := t418Daemon(t, dir)
	busy := &fakeSender{alive: true, inFlight: true}
	if _, err := deliverToSender(accepting, name, "held across the restart", false, busy, false); err != nil {
		t.Fatalf("queue: %v", err)
	}

	recovering, sender, _ := t418Daemon(t, dir)
	// No event sink, no terminal stop, nobody to press Enter: only the sweep.
	recovering.SweepSendBacklogs()
	if got := sender.delivered(); len(got) != 1 || got[0] != "held across the restart" {
		t.Fatalf("sweep delivered %v; want the held message", got)
	}
}

// Clause 5's sibling for the send queue: a backlog addressed to a seat that no
// longer exists is reported and dropped. Kept forever, it is neither delivered
// nor surfaced — the state clause 9 forbids.
func TestBacklogForADepartedAgentIsSurfacedThenDropped(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, _, up := t418Daemon(t, dir)
	s.registry = reg // an empty fleet: the addressee has been removed

	if _, _, err := s.sendQueue().Append("jv-departed", "reply to your question", time.Now()); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	s.SweepSendBacklogs()

	if !containsLine(up.all(), "UNDELIVERED") {
		t.Fatalf("a queue for a departed agent was dropped silently; overseer saw %q", up.all())
	}
	if depth := s.pendingAgentSends("jv-departed"); depth != 0 {
		t.Fatalf("depth = %d; want 0 — a queue against a seat nobody will fill is not pending", depth)
	}
}

// The reporting threshold, in both directions. A queue thirty seconds behind a
// working agent is a queue doing its job; one that has not moved in hours is
// the state six stacked messages sat in while every send reported success.
func TestOnlyAStalledBacklogRaisesANotice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		age   time.Duration
		alarm bool
	}{
		{"fresh queue behind a working agent", 30 * time.Second, false},
		{"queue that has not moved in hours", 3 * time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, up := t418Daemon(t, t.TempDir())
			const agent = "jv-busy"
			if _, _, err := s.sendQueue().Append(agent, "waiting", time.Now().Add(-tc.age)); err != nil {
				t.Fatalf("seed queue: %v", err)
			}
			s.noteTurnInFlight(agent)

			s.SweepSendBacklogs()

			raised := containsLine(up.all(), "Stalled backlog")
			if raised != tc.alarm {
				t.Fatalf("stall notice = %v, want %v; overseer saw %q", raised, tc.alarm, up.all())
			}
			if depth := s.pendingAgentSends(agent); depth != 1 {
				t.Fatalf("depth = %d; want 1 — a busy agent's queue is held, never dropped", depth)
			}
		})
	}
}

// The honesty clause the durable queue introduces: if the daemon could not
// write the message down, it has not queued it, and saying "queued (1 pending)
// … held by the daemon" would be a claim about a store that does not contain
// it.
func TestAQueueThatCannotBeWrittenIsReportedNotAnsweredQueued(t *testing.T) {
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	// A directory where the agent's record has to go: every write fails.
	if err := os.MkdirAll(filepath.Join(dir, "sendq", "jv-worker.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	busy := &fakeSender{alive: true, inFlight: true}
	res, err := deliverToSender(s, "jv-worker", "this one is not held", false, busy, false)
	if err == nil {
		t.Fatalf("an unwritable queue answered %q/%d instead of failing", res.Status, res.Queued)
	}
	if !strings.Contains(err.Error(), "NOT DELIVERED and NOT QUEUED") {
		t.Fatalf("error = %q; want it to say plainly that nothing is holding the message", err)
	}
}

// A message that could not be delivered goes back to the HEAD of the queue with
// the age it was accepted with. Appending it would put it behind messages
// accepted later; re-stamping it would hide how long it has been waiting.
func TestUndeliverableDrainReturnsTheMessageToTheHeadWithItsAge(t *testing.T) {
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	s.SetSenderResolver(func(string) (agentSender, bool, error) { return nil, false, nil })
	const name = "jv-worker"

	accepted := time.Now().Add(-2 * time.Hour)
	if _, _, err := s.sendQueue().Append(name, "first", accepted); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, _, err := s.sendQueue().Append(name, "second", time.Now()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s.drainAgentSendQueue(name) // no live process: the head comes back

	entries, err := s.sendQueue().Snapshot(name)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(entries) != 2 || entries[0].Text != "first" {
		t.Fatalf("queue = %+v; want the undelivered message back at the head", entries)
	}
	if age := entries[0].Age(time.Now()); age < 2*time.Hour {
		t.Fatalf("head age = %v; want the original acceptance age, not a fresh stamp", age)
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// 🎯T527 — a parked agent's held queue must not prescribe interrupt=true.
// Sibling of T410/T414: the park is intentional stand-down, and interrupt
// would fight it. Working + stalled in-flight still gets the existing advice.
func TestT527ParkedStalledBacklogHasNoInterruptPrescription(t *testing.T) {
	dir := t.TempDir()
	const agent = "jevons-po"

	s, _, up := t418Daemon(t, dir)
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	if err := store.SetAgent(agent, fleetintent.Parked, "jevons", "open-objective thrash", now); err != nil {
		t.Fatal(err)
	}

	// N queued messages, oldest well past the stall threshold; no live process
	// (parked seats are stopped). Also leave a stale in-flight flag — the
	// defect was diagnosing park as "turn still in flight".
	for i := 0; i < 3; i++ {
		if _, _, err := s.sendQueue().Append(agent, fmt.Sprintf("queued-%d", i), time.Now().Add(-3*time.Hour)); err != nil {
			t.Fatalf("seed queue: %v", err)
		}
	}
	s.noteTurnInFlight(agent)
	s.SetSenderResolver(func(string) (agentSender, bool, error) { return nil, false, nil })

	s.SweepSendBacklogs()

	text := strings.Join(up.all(), "\n")
	if !containsLine(up.all(), "Stalled backlog") {
		t.Fatalf("parked stalled backlog raised no notice; overseer saw %q", text)
	}
	if strings.Contains(text, "interrupt=true") {
		t.Fatalf("parked backlog prescribed interrupt=true — fights T414 park; overseer saw %q", text)
	}
	if !strings.Contains(text, "intentional stand-down") && !strings.Contains(text, "stood down") {
		t.Fatalf("parked backlog did not name the park; overseer saw %q", text)
	}
	if depth := s.pendingAgentSends(agent); depth != 3 {
		t.Fatalf("depth = %d; want 3 — park holds the queue, never drops it", depth)
	}
}

func TestT527WorkingStalledInFlightStillPrescribesInterrupt(t *testing.T) {
	s, _, up := t418Daemon(t, t.TempDir())
	const agent = "jv-busy"
	if _, _, err := s.sendQueue().Append(agent, "waiting", time.Now().Add(-3*time.Hour)); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	s.noteTurnInFlight(agent)

	s.SweepSendBacklogs()

	text := strings.Join(up.all(), "\n")
	if !containsLine(up.all(), "Stalled backlog") {
		t.Fatalf("working stalled backlog raised no notice; overseer saw %q", text)
	}
	if !strings.Contains(text, "interrupt=true") {
		t.Fatalf("working stalled in-flight lost the interrupt prescription; overseer saw %q", text)
	}
	if strings.Contains(text, "intentional stand-down") || strings.Contains(text, "stood down") {
		t.Fatalf("working agent was described as stood down; overseer saw %q", text)
	}
}

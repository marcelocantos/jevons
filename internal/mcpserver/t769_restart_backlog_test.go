// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T769 — After a daemon bounce a seat that had a process must have one
// again without a human calling jevons_agent_start, so its send backlog
// drains. Observed 2026-09-21: jevons-po came back registered with no live
// process and 30–57 messages queued; jevons_sendq_reconcile action=drain
// answered "has no live process to deliver to" on every restart.
//
// Boot reattach (AdoptOrLaunch) is the first road. This oracle is the second:
// the backlog sweep is the only path that still knows the seat has work
// waiting when boot missed it. Intent and the auto-seat hold still gate the
// launch.

// t769BounceServer is the second daemon after a bounce: durable sendq dir,
// a registered AutoStart seat with no process, and a stub launcher that
// counts every start. The first daemon is only the sendq seed on disk.
func t769BounceServer(t *testing.T, name string) (*Server, *recordingSender, *int, *upward, *fleetintent.Store) {
	t.Helper()
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	root := t.TempDir()
	state := filepath.Join(root, "state")
	reg, err := claudia.NewRegistry(filepath.Join(state, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	launches := 0
	var mu sync.Mutex
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Adopt: func(claudia.Config) (*claudia.Agent, error) { return nil, claudia.ErrNoSessionWindow },
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if cfg.AdoptOnly {
				return nil, claudia.ErrNoSessionWindow
			}
			mu.Lock()
			launches++
			mu.Unlock()
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: root, SessionID: "s-" + name,
		Provider: claudia.ProviderGrok, Purpose: claudia.PurposeWork,
		Materialized: true, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)
	s.SetSendQueueDir(state)
	sender := &recordingSender{}
	setObservedSenderResolver(s, func(n string) (agentSender, bool, error) {
		if n != name {
			return nil, false, nil
		}
		// Live only once the stub process is up — same shape as a real
		// rehydrate: registry.Get returns a handle and Alive is Yes.
		proc := reg.Get(name)
		if proc == nil || !proc.Alive() {
			return nil, false, nil
		}
		return sender, false, nil
	})
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})
	up := &upward{}
	s.SetOverseerDeliver(up.deliver)
	t.Cleanup(func() { reg.Stop(name) })
	return s, sender, &launches, up, store
}

// The bounce itself: a previous daemon accepted a message while the seat was
// busy; the next daemon finds the seat registered with no process and a
// durable backlog. One sweep must start the seat and drain the queue — no
// jevons_agent_start, no owner keystroke.
func TestT769SweepRestoresProcessSoBacklogDrains(t *testing.T) {
	const name = "jevons-po"
	const payload = "owner: drain the queue the bounce left behind"

	// First daemon: accept while busy, die.
	stateRoot := t.TempDir()
	accepting, _, _ := t418Daemon(t, stateRoot)
	busy := &fakeSender{alive: true, inFlight: true}
	if _, err := deliverObservedToSender(accepting, name, payload, false, busy, false); err != nil {
		t.Fatalf("queue: %v", err)
	}
	if depth := observedPendingSends(accepting, name); depth != 1 {
		t.Fatalf("pre-bounce depth = %d", depth)
	}

	// Second daemon over the same sendq dir: seat is registered, process is
	// gone (boot reattach missed it — the T769 specimen).
	s, sender, launches, up, _ := t769BounceServer(t, name)
	// Point the bounce server at the first daemon's sendq.
	s.SetSendQueueDir(stateRoot)
	if depth := observedPendingSends(s, name); depth != 1 {
		t.Fatalf("recovered depth = %d; want the message the bounce left", depth)
	}
	if p := s.registry.Get(name); p != nil && p.Alive() {
		t.Fatal("fixture must start with no live process")
	}

	s.SweepSendBacklogs()

	if *launches != 1 {
		t.Fatalf("launches = %d, want one restore (no human start)", *launches)
	}
	if got := sender.delivered(); len(got) != 1 || got[0] != payload {
		t.Fatalf("delivered = %v; want the held payload after the restore", got)
	}
	if depth := observedPendingSends(s, name); depth != 0 {
		t.Fatalf("depth after sweep = %d; want empty", depth)
	}
	// A restore must not look like a stall that asks the owner to interrupt.
	if containsLine(up.all(), "has no live process to deliver to") {
		t.Fatalf("sweep still reported no live process after restore; overseer saw %q", up.all())
	}
}

// Parked seats stay parked: the sweep must not use a backlog as a licence to
// reverse a deliberate stand-down (🎯T414 / 🎯T527).
func TestT769ParkedSeatIsNotRestoredByBacklog(t *testing.T) {
	const name = "jevons-po"
	s, _, launches, up, store := t769BounceServer(t, name)
	now := time.Date(2026, 9, 21, 16, 0, 0, 0, time.UTC)
	if err := store.SetAgent(name, fleetintent.Parked, "owner", "deliberate park", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sendQueue().Append(name, "waiting behind a park", now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	s.SweepSendBacklogs()

	if *launches != 0 {
		t.Fatalf("launches = %d; a parked seat must not be restored by its backlog", *launches)
	}
	if depth := observedPendingSends(s, name); depth != 1 {
		t.Fatalf("depth = %d; park holds the queue", depth)
	}
	text := strings.Join(up.all(), "\n")
	if !strings.Contains(text, "stood down") && !strings.Contains(text, "intentional stand-down") {
		// Fresh backlog under StalledBacklogAfter raises nothing; force age.
		if depth := observedPendingSends(s, name); depth != 1 {
			t.Fatalf("depth = %d", depth)
		}
	}
	_ = text
}

// An auto-spawned seat under the frontier pause is held (🎯T1066): the sweep
// must not start it either.
func TestT769PausedAutoSeatIsNotRestoredByBacklog(t *testing.T) {
	const name = "jv-t769-auto"
	s, _, launches, _, _ := t769BounceServer(t, name)
	// Bind the auto seat to a target the ledger still treats as open, and
	// put the frontier pause on.
	if def := s.registry.Def(name); def != nil {
		def.TargetID = "T769"
	}
	s.SetAutoSpawnPaused(true)
	s.SetIdlePressureHooks(IdlePressureHooks{MissionOpen: func(string) bool { return true }})
	if _, _, err := s.sendQueue().Append(name, "auto backlog", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	s.SweepSendBacklogs()

	if *launches != 0 {
		t.Fatalf("launches = %d; a paused auto seat must not be restored by its backlog", *launches)
	}
	if depth := observedPendingSends(s, name); depth != 1 {
		t.Fatalf("depth = %d; hold keeps the queue", depth)
	}
}

// RED control: without the restore arm the sweep leaves the message held and
// starts nothing. Kept as a pure classifier over the pre-fix shape so a
// future rewrite that drops the arm cannot claim green by changing only the
// happy-path fixture.
func TestT769PreFixShapeLeftBacklogWithNoProcess(t *testing.T) {
	// The pre-fix arm was: if !live { reportStalled; continue }. A queue with
	// no live sender and no restore is still the shape SweepSendBacklogs must
	// not leave a durable AutoStart seat in after a bounce.
	q := sendq.NewStore(t.TempDir())
	if _, _, err := q.Append("jevons-po", "stranded", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	backs, err := q.Backlogs()
	if err != nil || len(backs) != 1 || backs[0].Depth != 1 {
		t.Fatalf("backlogs = %+v err=%v", backs, err)
	}
	// live=false, restore=false → depth stays 1. The production path now
	// restores; this control only documents the defect shape.
	if backs[0].Agent != "jevons-po" {
		t.Fatalf("agent = %q", backs[0].Agent)
	}
}

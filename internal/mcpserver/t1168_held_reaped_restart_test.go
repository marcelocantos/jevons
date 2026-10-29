// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T1168 — 2026-10-22: held-backlog notices that 🎯T706 / 🎯T582 already
// released once were re-delivered on every development daemon bounce because
// the seam lived only in process memory.

func t582RestartFromDir(t *testing.T, dir string, agent, parent, reason string, now time.Time) *t582Fixture {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := eventlog.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)
	s.SetSendQueueDir(dir)
	s.SetRecoverBin("", dir)
	s.SetRemovalAccount(fleetlog.New(nil))
	s.SetEventJournal(journal)

	f := &t582Fixture{s: s, up: &upward{}, parent: &recordingSender{}, journal: journal, now: now}
	s.SetSweepClock(func() time.Time { return f.now })
	s.SetOverseerDeliver(f.up.deliver)
	setObservedSenderResolver(s, func(name string) (agentSender, bool, error) {
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
	if _, ok := LookupReapedRecord(s.fleetIntent(), agent); !ok {
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
	}
	return f
}

func TestT1168KilledSeatHeldBacklogNoticeSurvivesRestart(t *testing.T) {
	const agent, parent = "jv-t1158-auto", "jevons-po"
	dir := t.TempDir()
	now := time.Date(2026, 10, 22, 0, 52, 0, 0, time.UTC)
	f := t582RestartFromDir(t, dir, agent, parent, fleetlog.ReasonKill, now)

	if _, _, err := f.s.sendQueue().Append(agent,
		"gate feedback after kill",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.sweepFor(5 * time.Minute)
	if got := heldReapedNotices(f.up.all()); got != 1 {
		t.Fatalf("before restart: overseer notices = %d; want 1\n%s", got, strings.Join(f.up.all(), "\n"))
	}

	f = t582RestartFromDir(t, dir, agent, parent, fleetlog.ReasonKill, f.now)
	f.sweepFor(30 * time.Minute)
	if got := heldReapedNotices(f.up.all()); got != 0 {
		t.Fatalf("after restart: new overseer notices = %d; want 0\n%s", got, strings.Join(f.up.all(), "\n"))
	}
	backlogs, err := f.s.sendQueue().Backlogs()
	if err != nil || len(backlogs) != 1 || backlogs[0].Depth != 1 {
		t.Fatalf("hold still on disk: %+v err=%v", backlogs, err)
	}
}

func TestT1168FinishedSeatInformNoticeSurvivesRestart(t *testing.T) {
	const agent, parent = "jv-t1151.1-worktree-only", "jevons-po"
	dir := t.TempDir()
	now := time.Date(2026, 10, 22, 1, 8, 0, 0, time.UTC)
	f := t582RestartFromDir(t, dir, agent, parent, fleetlog.ReasonReapAchieve, now)

	if _, _, err := f.s.sendQueue().Append(agent,
		"owner: post-achieve follow-up for the worktree",
		f.now.Add(-heldReapedRouteAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.sweepFor(5 * time.Minute)
	_ = finishedSeatInformText(t, f, 1)

	f = t582RestartFromDir(t, dir, agent, parent, fleetlog.ReasonReapAchieve, f.now)
	f.sweepFor(30 * time.Minute)
	if got := heldReapedNotices(f.up.all()); got != 0 {
		t.Fatalf("overseer notices after restart = %d; want 0", got)
	}
	if got := len(heldReapedInformTexts(t, f.journal)); got != 1 {
		t.Fatalf("event-log inform notices after restart = %d; want still 1", got)
	}
}

func TestT1168NewHoldOnSameSeatAnnouncesAfterRestart(t *testing.T) {
	const agent, parent = "jv-t679.1-evidence-seam", "jevons-po"
	dir := t.TempDir()
	now := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	f := t582RestartFromDir(t, dir, agent, parent, fleetlog.ReasonKill, now)
	q := f.s.sendQueue()

	if _, _, err := q.Append(agent, "first hold",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.sweepFor(5 * time.Minute)
	if got := heldReapedNotices(f.up.all()); got != 1 {
		t.Fatalf("first hold notices = %d; want 1", got)
	}

	for _, id := range mustBacklogEntryIDs(t, q, agent) {
		e, claimed, err := q.ClaimFrontID(agent, id)
		if err != nil || !claimed {
			t.Fatalf("claim %s: claimed=%v err=%v", id, claimed, err)
		}
		if err := q.Resolve(agent, e, sendq.Confirmed, "drained"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := q.Append(agent, "second hold",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}

	f = t582RestartFromDir(t, dir, agent, parent, fleetlog.ReasonKill, f.now)
	f.sweepFor(5 * time.Minute)
	if got := heldReapedNotices(f.up.all()); got != 1 {
		t.Fatalf("second hold notices after restart = %d; want 1\n%s", got, strings.Join(f.up.all(), "\n"))
	}
}

func mustBacklogEntryIDs(t *testing.T, q *sendq.Store, agent string) []string {
	t.Helper()
	backlogs, err := q.Backlogs()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range backlogs {
		if b.Agent == agent {
			return b.EntryIDs
		}
	}
	t.Fatalf("no backlog for %q", agent)
	return nil
}

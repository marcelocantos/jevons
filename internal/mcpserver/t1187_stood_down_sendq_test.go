// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T1187 — development jevonsd.log repeated 🎯T418 backlog stalled WARNs every
// sweep for reaped/parked seats whose sendq cannot drain under fleet intent.
// The clock is injected so twenty minutes of sweeps is arithmetic.

func countStallWarns(records []slog.Record) int {
	n := 0
	for _, r := range records {
		if r.Level != slog.LevelWarn {
			continue
		}
		if strings.Contains(r.Message, "🎯T418 backlog stalled") {
			n++
		}
	}
	return n
}

func TestT1187ReapedBacklogSettlesWithoutRepeatingStallWarns(t *testing.T) {
	const agent, parent = "jv-t1074.1-auto", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonReapAchieve)

	capture := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(prev)

	if _, _, err := f.s.sendQueue().Append(agent,
		"owner: integrate the worker report when you have a moment",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}

	f.sweepFor(20 * time.Minute)

	if got := countStallWarns(capture.snapshot()); got > 1 {
		t.Fatalf("stall WARN lines = %d over 20 simulated minutes; want at most 1", got)
	}
	if got := heldReapedNotices(f.up.all()); got > 1 {
		t.Fatalf("fleet-health notices = %d; want at most 1\n%s", got, strings.Join(f.up.all(), "\n"))
	}
	if got := len(f.parent.delivered()); got != 1 {
		t.Fatalf("parent received %d message(s); want one routed hold", got)
	}
	rows, err := f.s.sendQueue().Snapshot(agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].State != sendq.Uncertain {
		t.Fatalf("sendq after settle = %+v; want one uncertain routed entry", rows)
	}
}

func TestT1187ReapedGateFeedbackIsDroppedOnce(t *testing.T) {
	const agent, parent = "jv-t1074.1-auto", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonReapAchieve)

	capture := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(prev)

	payload := "GATE make-test-go exit=1 SUSPECT id=deadbeef — master is red on your commit"
	if _, _, err := f.s.sendQueue().Append(agent, payload, f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}

	f.sweepFor(20 * time.Minute)

	if got := countStallWarns(capture.snapshot()); got > 1 {
		t.Fatalf("stall WARN lines = %d; want at most 1", got)
	}
	if got := heldReapedNotices(f.up.all()); got > 1 {
		t.Fatalf("fleet-health notices = %d; want at most 1", got)
	}
	backlogs, err := f.s.sendQueue().Backlogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(backlogs) != 0 {
		t.Fatalf("sendq depth after drop = %+v; want empty", backlogs)
	}
	events, err := eventlog.Tail(f.journal.Path(), eventlog.TailOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var dropped int
	for _, e := range events {
		if e.Component == "agent_send" && e.Decision == "held_backlog_dropped" {
			dropped++
		}
	}
	if dropped != 1 {
		t.Fatalf("held_backlog_dropped events = %d; want 1", dropped)
	}
}

func TestT1187ParkedHoldWarnsAtMostOnce(t *testing.T) {
	const agent = "jv-t1054-noise-classifier-scout"
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 14, 5, 45, 26, 0, time.UTC)

	capture := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(prev)

	s, _, up := t418Daemon(t, dir)
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	if err := store.SetAgent(agent, fleetintent.Parked, "jevons-po", "parked scout", t0); err != nil {
		t.Fatal(err)
	}
	entry, _, err := s.sendQueue().Append(agent, "parked obligation", t0.Add(-StalledBacklogAfter-time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	for elapsed := time.Duration(0); elapsed <= 20*time.Minute; elapsed += 30 * time.Second {
		now := t0.Add(elapsed)
		s.SetSweepClock(func() time.Time { return now })
		s.SweepSendBacklogs()
	}

	if got := countStallWarns(capture.snapshot()); got > 1 {
		t.Fatalf("stall WARN lines = %d over 20 simulated minutes; want at most 1", got)
	}
	if got := up.all(); len(got) != 0 {
		t.Fatalf("parked hold prompted overseer: %v", got)
	}
	rows, err := s.sendQueue().Snapshot(agent)
	if err != nil || len(rows) != 1 || rows[0].ID != entry.ID {
		t.Fatalf("parked hold changed: %+v %v", rows, err)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

func TestT952SweepClockFileIsIsolateOnly(t *testing.T) {
	dir := t.TempDir()
	clock := filepath.Join(dir, "now")
	ancient := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.WriteFile(clock, []byte(ancient.Format(time.RFC3339Nano)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(IsolateSweepNowFileEnv, clock)
	s := &Server{}
	got := s.sweepClock()
	if got.Equal(ancient) || time.Since(got) > time.Minute {
		t.Fatalf("wall-clock sweep read the isolate file: %s", got)
	}
	t.Setenv(isolateTestFaultsEnv, "1")
	if got = s.sweepClock(); !got.Equal(ancient) {
		t.Fatalf("isolate sweep clock = %s, file says %s", got, ancient)
	}
}

// The isolate journey advances this same file. Twenty simulated minutes and a
// restart must not prompt a parked seat, and the working stall's first alert
// stays inside the two age-band deliveries.
func TestT952IsolateClockSweepsTwentyMinutes(t *testing.T) {
	dir := t.TempDir()
	clock := filepath.Join(dir, "now")
	t0 := time.Date(2026, 10, 14, 5, 45, 26, 0, time.UTC)
	if err := os.WriteFile(clock, []byte(t0.Format(time.RFC3339Nano)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(isolateTestFaultsEnv, "1")
	t.Setenv(IsolateSweepNowFileEnv, clock)

	const hold = "t952-hold"
	const stall = "t952-stall"
	up := &upward{}
	s := t952ClockDaemon(t, dir, up)
	if _, _, err := s.sendQueue().Append(hold, "parked obligation", t0.Add(-(77*time.Hour + 6*time.Minute + 41*time.Second))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sendQueue().Append(stall, "stall obligation", t0.Add(-(10*time.Minute + 25*time.Second))); err != nil {
		t.Fatal(err)
	}
	if err := s.intentStore().SetAgent(hold, fleetintent.Parked, "owner", "intentional hold", t0); err != nil {
		t.Fatal(err)
	}

	for minute := 0; minute <= 20; minute++ {
		if minute == 10 {
			s = t952ClockDaemon(t, dir, up)
		}
		now := t0.Add(time.Duration(minute) * time.Minute)
		if err := os.WriteFile(clock, []byte(now.Format(time.RFC3339Nano)), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := s.sweepClock(); !got.Equal(now) {
			t.Fatalf("minute %d clock = %s", minute, got)
		}
		s.SweepSendBacklogs()
	}

	var stalled []string
	for _, line := range up.all() {
		if strings.Contains(line, hold) {
			t.Fatalf("parked hold was submitted: %q", line)
		}
		if strings.Contains(line, "Stalled backlog") && strings.Contains(line, stall) {
			stalled = append(stalled, line)
		}
	}
	if len(stalled) != 2 {
		t.Fatalf("stall notices=%d want 2 (open and the 15m band); overseer saw %v", len(stalled), up.all())
	}
	for _, name := range []string{hold, stall} {
		rows, err := s.sendQueue().Snapshot(name)
		if err != nil || len(rows) != 1 || rows[0].Text == "" {
			t.Fatalf("%s obligation changed: %+v %v", name, rows, err)
		}
	}
}

func t952ClockDaemon(t *testing.T, dir string, up *upward) *Server {
	t.Helper()
	s := &Server{}
	s.SetSendQueueDir(dir)
	s.SetRecoverBin("", dir)
	// No sender and no registry: a registered-but-never-started isolate seat
	// has no live process, so the stall stays a notice instead of a drain.
	s.SetOverseerDeliver(up.deliver)
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	return s
}

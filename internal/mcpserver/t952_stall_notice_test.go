// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"
)

// 🎯T952 — pure backoff oracle + hermetic 20-minute WARN-cadence sweep.

func TestDecideStallNoticeFirstThenBackoff(t *testing.T) {
	now := time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC)
	shape := StallNoticeShape{Agent: "claudia-po", Depth: 1, Why: "its turn is still in flight", AgeBand: 0}

	admit, st := DecideStallNotice(StallNoticeState{}, shape, now)
	if !admit || st.Notices != 1 || st.Interval != StallNoticeInitialBackoff {
		t.Fatalf("first: admit=%v st=%+v", admit, st)
	}

	// One minute later, same shape: still inside the initial backoff.
	admit, st2 := DecideStallNotice(st, shape, now.Add(time.Minute))
	if admit || st2.Notices != 1 {
		t.Fatalf("inside backoff: admit=%v st=%+v", admit, st2)
	}

	// At the backoff boundary: admit and double.
	admit, st3 := DecideStallNotice(st, shape, now.Add(StallNoticeInitialBackoff))
	if !admit || st3.Notices != 2 || st3.Interval != 2*StallNoticeInitialBackoff {
		t.Fatalf("after backoff: admit=%v st=%+v", admit, st3)
	}
}

func TestDecideStallNoticeShapeChangeAdmitsImmediately(t *testing.T) {
	now := time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC)
	shape := StallNoticeShape{Agent: "claudia-po", Depth: 1, Why: "its turn is still in flight", AgeBand: 0}
	_, st := DecideStallNotice(StallNoticeState{}, shape, now)

	// Depth change 30s later — news, not a tick.
	shape2 := shape
	shape2.Depth = 2
	admit, next := DecideStallNotice(st, shape2, now.Add(30*time.Second))
	if !admit || next.Notices != 2 || next.Interval != StallNoticeInitialBackoff {
		t.Fatalf("depth change: admit=%v next=%+v", admit, next)
	}

	// Age-band crossing is also news.
	_, st = DecideStallNotice(StallNoticeState{}, shape, now)
	shape3 := shape
	shape3.AgeBand = 1
	admit, next = DecideStallNotice(st, shape3, now.Add(30*time.Second))
	if !admit || next.Notices != 2 {
		t.Fatalf("band change: admit=%v next=%+v", admit, next)
	}
}

func TestDecideStallNoticeCapsBackoff(t *testing.T) {
	now := time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC)
	shape := StallNoticeShape{Agent: "a", Depth: 1, Why: "x", AgeBand: 0}
	st := StallNoticeState{
		LastAt:   now,
		Interval: StallNoticeMaxBackoff,
		Shape:    shape,
		Notices:  5,
	}
	admit, next := DecideStallNotice(st, shape, now.Add(StallNoticeMaxBackoff))
	if !admit || next.Interval != StallNoticeMaxBackoff {
		t.Fatalf("cap: admit=%v next=%+v", admit, next)
	}
}

func TestStallNoticeAgeBandMatchesT810(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want int
	}{
		{10 * time.Minute, 0},
		{15 * time.Minute, 1},
		{time.Hour, 2},
		{6 * time.Hour, 3},
		{24 * time.Hour, 4},
	}
	for _, tc := range cases {
		if got := StallNoticeAgeBand(tc.age); got != tc.want {
			t.Fatalf("age=%s band=%d want %d", tc.age, got, tc.want)
		}
	}
}

// The load-bearing hermetic: 20 simulated minutes of WARN-cadence sweeps
// (~60 s) against one stable in-flight stall. Overseer notices are bounded
// (≤ 4) and each admitted line carries a growing age.
func TestT952TwentyMinuteStableStallIsBounded(t *testing.T) {
	s, _, up := t418Daemon(t, t.TempDir())
	const agent = "claudia-po"

	// Accepted 10 minutes before t0 so the first sweep is already past
	// StalledBacklogAfter — the same shape as the 2026-09-30 specimen.
	t0 := time.Date(2026, 9, 30, 2, 6, 0, 0, time.UTC)
	accepted := t0.Add(-10 * time.Minute)
	if _, _, err := s.sendQueue().Append(agent, "held behind a long turn", accepted); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s.noteTurnInFlight(agent)

	var now time.Time
	s.SetSweepClock(func() time.Time { return now })
	// Confirm fleet-health deliveries so T810 does not independently re-offer
	// inside the unconfirmed grace — T952 is the gate under test.
	s.SetTurnWitness(func(string, string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, PayloadSeen: true, Durable: true}
		}
	})

	const warnCadence = 60 * time.Second
	const horizon = 20 * time.Minute
	var ages []string
	for elapsed := time.Duration(0); elapsed <= horizon; elapsed += warnCadence {
		now = t0.Add(elapsed)
		before := len(up.all())
		s.SweepSendBacklogs()
		got := up.all()
		if len(got) > before {
			last := got[len(got)-1]
			if !strings.Contains(last, "Stalled backlog") {
				t.Fatalf("unexpected notice: %q", last)
			}
			ages = append(ages, last)
		}
	}

	got := up.all()
	if n := len(got); n == 0 || n > 4 {
		t.Fatalf("overseer notices=%d want 1..4 over 20m WARN cadence; lines=%v", n, got)
	}
	// Notices carry growing age: each admitted line's age token is later than
	// the previous one's (the stall did not shrink).
	var prevAge time.Duration
	for i, line := range ages {
		age := t952ExtractAge(t, line)
		if i > 0 && age < prevAge {
			t.Fatalf("age went backwards: %s then %s\n%s", prevAge, age, line)
		}
		prevAge = age
	}

	// Control: without the gate the same fixture would fire every sweep after
	// the stall threshold — the pure Decide path already proves the doubling;
	// this check pins the integrated count stays well under the WARN tick count.
	ticks := int(horizon/warnCadence) + 1
	if len(got) >= ticks/2 {
		t.Fatalf("notices=%d is not a backoff relative to %d WARN ticks", len(got), ticks)
	}
}

func TestT952DepthChangeReNotifiesInsideBackoff(t *testing.T) {
	s, _, up := t418Daemon(t, t.TempDir())
	const agent = "claudia-po"
	t0 := time.Date(2026, 9, 30, 2, 6, 0, 0, time.UTC)
	accepted := t0.Add(-3 * time.Hour)
	if _, _, err := s.sendQueue().Append(agent, "one", accepted); err != nil {
		t.Fatal(err)
	}
	s.noteTurnInFlight(agent)
	var now time.Time
	s.SetSweepClock(func() time.Time { return now })
	s.SetTurnWitness(func(string, string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, PayloadSeen: true, Durable: true}
		}
	})

	now = t0
	s.SweepSendBacklogs()
	if len(up.all()) != 1 {
		t.Fatalf("first notice: %d", len(up.all()))
	}

	// 30s later, same stall: suppressed.
	now = t0.Add(30 * time.Second)
	s.SweepSendBacklogs()
	if len(up.all()) != 1 {
		t.Fatalf("inside backoff still notified: %d", len(up.all()))
	}

	// A second message lands — depth is news.
	if _, _, err := s.sendQueue().Append(agent, "two", now); err != nil {
		t.Fatal(err)
	}
	s.SweepSendBacklogs()
	got := up.all()
	if len(got) != 2 {
		t.Fatalf("depth change did not re-notify: %d lines %v", len(got), got)
	}
	if !strings.Contains(got[1], "2 message") {
		t.Fatalf("second notice did not carry new depth: %q", got[1])
	}
}

// t952ExtractAge pulls the "oldest waiting <dur>" token from a stall line.
func t952ExtractAge(t *testing.T, line string) time.Duration {
	t.Helper()
	const marker = "the oldest waiting "
	i := strings.Index(line, marker)
	if i < 0 {
		t.Fatalf("no age in %q", line)
	}
	rest := line[i+len(marker):]
	end := strings.IndexAny(rest, ",. ")
	if end < 0 {
		end = len(rest)
	}
	tok := rest[:end]
	d, err := time.ParseDuration(tok)
	if err != nil {
		t.Fatalf("age %q in %q: %v", tok, line, err)
	}
	return d
}

// Sanity: the pure 20-minute schedule itself never exceeds 4 admissions.
func TestDecideStallNoticeTwentyMinuteSchedule(t *testing.T) {
	now := time.Date(2026, 9, 30, 2, 0, 0, 0, time.UTC)
	shape := StallNoticeShape{Agent: "a", Depth: 1, Why: "turn", AgeBand: 0}
	var st StallNoticeState
	admitted := 0
	for elapsed := time.Duration(0); elapsed <= 20*time.Minute; elapsed += time.Minute {
		var ok bool
		ok, st = DecideStallNotice(st, shape, now.Add(elapsed))
		if ok {
			admitted++
		}
	}
	if admitted == 0 || admitted > 4 {
		t.Fatalf("pure schedule admissions=%d want 1..4", admitted)
	}
}

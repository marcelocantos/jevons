// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatstop

import (
	"strings"
	"testing"
	"time"
)

// The 2026-09-15 fixture: five seats found stopped within a minute, no
// daemon restart, nothing recorded why.
func t662Fixture(base time.Time) []Record {
	names := []string{"jv-t657-steer-ui", "jv-t658-hop-classifier", "jv-t659-clean-web-gate", "jv-t661-oversized-seat", "claudia-po"}
	var rs []Record
	for i, n := range names {
		rs = append(rs, Record{Seat: n, At: base.Add(time.Duration(i*9) * time.Second), Source: SourceExit, Reason: Unknown})
	}
	return rs
}

func TestT662MassStopNamesUnknownRatherThanSilence(t *testing.T) {
	base := time.Date(2026, 9, 15, 10, 52, 0, 0, time.UTC)
	boot := base.Add(-9 * time.Minute) // the 20:43 local restart, before the burst
	a, ok := MassStop(t662Fixture(base), DefaultWindow, DefaultMinSeats, boot)
	if !ok {
		t.Fatal("five seats within a minute is a mass stop")
	}
	if len(a.Seats) != 5 || a.Seats[0] != "claudia-po" {
		t.Fatalf("seats = %v", a.Seats)
	}
	if a.Shared != "unknown: 5 seats, no reason recorded" {
		t.Fatalf("shared = %q", a.Shared)
	}
	line := FormatAlert(a)
	for _, want := range []string{"MASS STOP", "5 seats", "no daemon restart", "unknown: 5 seats, no reason recorded", "jv-t658-hop-classifier"} {
		if !strings.Contains(line, want) {
			t.Fatalf("alert missing %q: %s", want, line)
		}
	}
}

func TestT662RestartInsideTheBurstIsNotAMassStop(t *testing.T) {
	base := time.Date(2026, 9, 15, 11, 5, 0, 0, time.UTC)
	boot := base.Add(20 * time.Second)
	if _, ok := MassStop(t662Fixture(base), DefaultWindow, DefaultMinSeats, boot); ok {
		t.Fatal("a daemon restart inside the window is the restart path's story, not a mass stop")
	}
}

func TestT662SharedReasonIsNamed(t *testing.T) {
	base := time.Date(2026, 9, 15, 11, 5, 0, 0, time.UTC)
	rs := []Record{
		{Seat: "a", At: base, Source: SourcePlanPolicy, Reason: "plan policy: claude weekly exhausted — parked"},
		{Seat: "b", At: base.Add(5 * time.Second), Source: SourcePlanPolicy, Reason: "plan policy: claude weekly exhausted — parked"},
		{Seat: "c", At: base.Add(30 * time.Second), Source: SourcePlanPolicy, Reason: "plan policy: claude weekly exhausted — parked"},
	}
	a, ok := MassStop(rs, DefaultWindow, DefaultMinSeats, time.Time{})
	if !ok || a.Shared != "plan policy: claude weekly exhausted — parked" {
		t.Fatalf("shared reason not named: %v %+v", ok, a)
	}
	rs = append(rs, Record{Seat: "d", At: base.Add(40 * time.Second), Source: SourceExit, Reason: Unknown})
	a, _ = MassStop(rs, DefaultWindow, DefaultMinSeats, time.Time{})
	if !strings.HasPrefix(a.Shared, "mixed:") || !strings.Contains(a.Shared, "unknown ×1") {
		t.Fatalf("mixed burst not described: %q", a.Shared)
	}
}

func TestT662TwoSeatsAreACoincidence(t *testing.T) {
	base := time.Date(2026, 9, 15, 11, 5, 0, 0, time.UTC)
	rs := t662Fixture(base)[:2]
	if _, ok := MassStop(rs, DefaultWindow, DefaultMinSeats, time.Time{}); ok {
		t.Fatal("two seats do not make a mass stop")
	}
}

func TestT662LedgerRecordsAndForgets(t *testing.T) {
	l := New()
	now := time.Date(2026, 9, 15, 11, 7, 0, 0, time.UTC)
	l.SetClock(func() time.Time { return now })
	r := l.Note(Record{Seat: "jv-x", Source: SourceSupervisor, Actor: "jevons", Reason: ""})
	if r.Reason != Unknown || !r.At.Equal(now) {
		t.Fatalf("empty reason must become Unknown with the clock stamped: %+v", r)
	}
	if got, ok := l.Last("jv-x"); !ok || got.Actor != "jevons" {
		t.Fatalf("Last = %+v %v", got, ok)
	}
	if n := len(l.Recent(now, time.Minute)); n != 1 {
		t.Fatalf("recent = %d", n)
	}
	l.Forget("jv-x")
	if _, ok := l.Last("jv-x"); ok {
		t.Fatal("Forget left the record")
	}
	if n := len(l.Recent(now, time.Minute)); n != 1 {
		t.Fatal("Forget must not erase history a mass-stop reading needs")
	}
}

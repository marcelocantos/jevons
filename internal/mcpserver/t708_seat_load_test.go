// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/seatload"
)

// The specimen as a process table: seat root 900 whose burn shell detached
// a loop (902) that outlived it and was reparented to init, keeping the
// seat's process group.
func t708Table() seatload.Table {
	return seatload.Table{
		{PID: 1, PPID: 0, PGID: 1, Command: "/sbin/launchd"},
		{PID: 500, PPID: 1, PGID: 500, Command: "jevonsd"},
		{PID: 900, PPID: 500, PGID: 900, Command: "codex --seat cl-t33-codex-load"},
		{PID: 902, PPID: 1, PGID: 900, CPUPercent: 190, Command: "/bin/sh -c while :; do go test -race ./...; done"},
	}
}

// 🎯T708: the stop path takes the seat's detached background work with it.
// Before this, jevons_agent_kill removed the registry row and left sixteen
// shells running on the owner's machine with nothing above them.
func TestT708KillPathReapsSeatBackgroundWork(t *testing.T) {
	var signalled []int
	s := &Server{}
	s.seatLoad = &seatload.Tracker{
		List:   func() (seatload.Table, error) { return t708Table(), nil },
		Signal: func(pid int, _ syscall.Signal) error { signalled = append(signalled, pid); return nil },
		Sleep:  func(time.Duration) {},
		Self:   500,
	}
	if _, err := s.seatLoad.Track("cl-t33-codex-load", 900); err != nil {
		t.Fatalf("Track: %v", err)
	}

	res := s.reapSeatLoad("cl-t33-codex-load")

	if len(signalled) == 0 {
		t.Fatal("the stop path signalled nothing; the seat's loops would outlive it")
	}
	for _, pid := range signalled {
		if pid == 500 || pid <= 1 {
			t.Fatalf("reap signalled %d — the daemon or init", pid)
		}
	}
	if res.Orphaned == 0 {
		t.Error("the reparented loop was not counted as orphaned, which is what makes it unreachable from the seat's turn")
	}
	// The anchor is spent: a second stop of the same name must not re-signal.
	signalled = nil
	if res2 := s.reapSeatLoad("cl-t33-codex-load"); len(signalled) != 0 {
		t.Fatalf("second reap signalled %v (%s)", signalled, res2)
	}
}

// A seat with nothing detached is reaped silently — no signals, no noise.
func TestT708QuietSeatReapsNothing(t *testing.T) {
	var signalled []int
	s := &Server{}
	s.seatLoad = &seatload.Tracker{
		List: func() (seatload.Table, error) {
			return seatload.Table{
				{PID: 1, PPID: 0, PGID: 1},
				{PID: 700, PPID: 1, PGID: 700, Command: "claude --seat jv-quiet"},
			}, nil
		},
		Signal: func(pid int, _ syscall.Signal) error { signalled = append(signalled, pid); return nil },
		Sleep:  func(time.Duration) {},
		Self:   500,
	}
	if _, err := s.seatLoad.Track("jv-quiet", 700); err != nil {
		t.Fatalf("Track: %v", err)
	}
	if res := s.reapSeatLoad("jv-quiet"); res.Any() || len(signalled) != 0 {
		t.Fatalf("quiet seat reap signalled %v (%s)", signalled, res)
	}
}

// 🎯T708 clause 3: at critical the governor acts on load already running.
// Before this, AdmitSpawn refused new panes correctly while the load that
// was starving the fleet was measured and tolerated.
func TestT708CriticalTerminatesUnreachableLoad(t *testing.T) {
	var signalled []int
	s := &Server{}
	s.seatLoad = &seatload.Tracker{
		List:   func() (seatload.Table, error) { return t708Table(), nil },
		Signal: func(pid int, _ syscall.Signal) error { signalled = append(signalled, pid); return nil },
		Sleep:  func(time.Duration) {},
		Self:   500,
	}
	if _, err := s.seatLoad.Track("cl-t33-codex-load", 900); err != nil {
		t.Fatalf("Track: %v", err)
	}
	acts := capacity.ActOnLoad(
		capacity.Assessment{Pressure: capacity.PressureCritical, LoadAverageHeadroom: 0},
		[]capacity.LoadSource{{
			Seat: "cl-t33-codex-load", Procs: 1, CPUPercent: 190,
			Unbounded: true, Orphaned: 1, SeatIdle: true,
			Heaviest: "/bin/sh -c while :; do go test -race ./...; done",
		}})
	if len(acts) != 1 || acts[0].Verdict != capacity.LoadTerminate {
		t.Fatalf("acts = %+v, want one terminate", acts)
	}

	s.applySeatLoadActions(acts)

	if len(signalled) == 0 {
		t.Fatal("critical terminate signalled nothing — the governor certified the outage it exists to prevent")
	}
	for _, pid := range signalled {
		if pid == 500 || pid <= 1 {
			t.Fatalf("terminate signalled %d — the daemon or init", pid)
		}
	}
}

// A live turn is told, not reached into: the seat can still bound its own
// work, and the daemon killing a running turn's children is a different
// and worse failure.
func TestT708CriticalNotifiesAReachableSeatWithoutSignalling(t *testing.T) {
	var signalled []int
	s := &Server{}
	s.seatLoad = &seatload.Tracker{
		List:   func() (seatload.Table, error) { return t708Table(), nil },
		Signal: func(pid int, _ syscall.Signal) error { signalled = append(signalled, pid); return nil },
		Sleep:  func(time.Duration) {},
		Self:   500,
	}
	if _, err := s.seatLoad.Track("cl-t33-codex-load", 900); err != nil {
		t.Fatalf("Track: %v", err)
	}
	acts := capacity.ActOnLoad(
		capacity.Assessment{Pressure: capacity.PressureCritical, LoadAverageHeadroom: 0},
		[]capacity.LoadSource{{Seat: "cl-t33-codex-load", Procs: 1, CPUPercent: 190}})
	if len(acts) != 1 || acts[0].Verdict != capacity.LoadNotify {
		t.Fatalf("acts = %+v, want one notify", acts)
	}
	s.applySeatLoadActions(acts)
	if len(signalled) != 0 {
		t.Fatalf("a seat still taking turns was signalled: %v", signalled)
	}
}

// A seat never anchored must not panic or signal anything.
func TestT708ReapOfUnanchoredSeatIsSafe(t *testing.T) {
	s := &Server{}
	if res := s.reapSeatLoad("never-seen"); res.Any() {
		t.Fatalf("unanchored reap did something: %s", res)
	}
}

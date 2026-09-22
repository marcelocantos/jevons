// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatload

import (
	"syscall"
	"testing"
	"time"
)

// The 2026-09-20 shape as a table: a seat root (900) whose shell (901)
// detached a loop (902) and then exited, so the loop is reparented to 1 and
// keeps the seat's process group. A sibling seat (800) and the daemon (500)
// are the controls a too-greedy selection would eat.
func specimenTable() Table {
	return Table{
		{PID: 1, PPID: 0, PGID: 1, Command: "/sbin/launchd"},
		{PID: 500, PPID: 1, PGID: 500, Command: "jevonsd"},
		{PID: 800, PPID: 500, PGID: 800, Command: "claude --seat other"},
		{PID: 900, PPID: 500, PGID: 900, CPUPercent: 3, Elapsed: time.Hour, Command: "codex --seat cl-t33-codex-load"},
		{PID: 901, PPID: 900, PGID: 900, CPUPercent: 1, Elapsed: 40 * time.Minute, Command: "/bin/sh scratchpad/t33/burn.sh"},
		{PID: 902, PPID: 1, PGID: 900, CPUPercent: 190, Elapsed: 40 * time.Minute, Command: "/bin/sh -c while :; do go test -race -count=1 ./...; done"},
		{PID: 903, PPID: 902, PGID: 900, CPUPercent: 95, Elapsed: 30 * time.Second, Command: "go test -race -count=1 ./..."},
	}
}

func TestT708DescendantsFindOrphanedGroupMembers(t *testing.T) {
	tbl := specimenTable()
	a, err := AnchorFor(tbl, "cl-t33-codex-load", 900, time.Now())
	if err != nil {
		t.Fatalf("AnchorFor: %v", err)
	}
	if a.PGID != 900 {
		t.Fatalf("anchor group = %d, want 900", a.PGID)
	}
	got := map[int]bool{}
	for _, p := range Descendants(tbl, a) {
		got[p.PID] = true
	}
	for _, want := range []int{901, 902, 903} {
		if !got[want] {
			t.Errorf("descendants missing %d: a parent walk alone loses the reparented loop, which is the whole defect", want)
		}
	}
	for _, never := range []int{1, 500, 800, 900} {
		if got[never] {
			t.Errorf("descendants include %d — the selection reaches outside the seat", never)
		}
	}
}

func TestT708SharedProcessGroupDoesNotClaimSiblings(t *testing.T) {
	// Three Cursor seats the broker placed in one group. None of them
	// leads it. Each reading must stay inside that seat's children.
	tbl := Table{
		{PID: 1, PPID: 0, PGID: 1, Command: "launchd"},
		{PID: 99272, PPID: 1, PGID: 99272, Command: "broker"},
		{PID: 24067, PPID: 99272, PGID: 99272, Command: "cursor-agent claudia-po"},
		{PID: 24068, PPID: 24067, PGID: 99272, Command: "claudia-po child"},
		{PID: 31741, PPID: 99272, PGID: 99272, CPUPercent: 90, Command: "cursor-agent cl-t119"},
		{PID: 31742, PPID: 31741, PGID: 99272, CPUPercent: 80, Command: "go test"},
		{PID: 22873, PPID: 99272, PGID: 99272, Command: "cursor-agent jv-t540"},
	}
	a, err := AnchorFor(tbl, "cl-t119-broker-gate", 31741, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]bool{}
	for _, p := range Descendants(tbl, a) {
		got[p.PID] = true
	}
	if !got[31742] {
		t.Fatal("own child missing")
	}
	for _, never := range []int{99272, 24067, 24068, 22873, 31741} {
		if got[never] {
			t.Errorf("shared group attributed pid %d to cl-t119", never)
		}
	}
}

func TestT708OrphanedNamesTheUnreachableLoops(t *testing.T) {
	tbl := specimenTable()
	a, _ := AnchorFor(tbl, "cl-t33-codex-load", 900, time.Now())
	orph := Orphaned(tbl, Descendants(tbl, a))
	if len(orph) != 1 || orph[0].PID != 902 {
		t.Fatalf("orphaned = %+v, want just pid 902", orph)
	}
}

func TestT708SummarizeReadsTheSpecimen(t *testing.T) {
	tbl := specimenTable()
	a, _ := AnchorFor(tbl, "cl-t33-codex-load", 900, time.Now())
	s := Summarize(tbl, a)
	if s.Procs != 3 {
		t.Errorf("procs = %d, want 3", s.Procs)
	}
	if s.CPUPercent < 285 {
		t.Errorf("cpu = %v, want the summed 286", s.CPUPercent)
	}
	if !s.Unbounded {
		t.Error("a `while :; do go test; done` loop must read as unbounded")
	}
	if s.Orphaned != 1 {
		t.Errorf("orphaned = %d, want 1", s.Orphaned)
	}
	if s.Oldest < 39*time.Minute {
		t.Errorf("oldest = %v, want the 40-minute loop", s.Oldest)
	}
}

func TestT708LooksUnboundedSpotsTheShapeNotTheProgram(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"/bin/sh -c while :; do go test -race ./...; done", true},
		{"/bin/bash -c 'while true; do make test; done'", true},
		// A bound that wraps the loop is a bound.
		{"timeout 900 /bin/sh -c 'while :; do go test ./...; done'", false},
		// A bound inside the loop body bounds one iteration, not the loop —
		// which is exactly what burn.sh's -count=1 did for forty minutes.
		{"/bin/sh -c 'while :; do timeout 60 go test ./...; done'", true},
		{"/bin/sh -c 'while :; do go test -race -count=1 ./...; done'", true},
		{"go test -race -count=1 ./...", false},
		{"/usr/bin/make test-go", false},
	}
	for _, c := range cases {
		if got := LooksUnbounded(c.cmd); got != c.want {
			t.Errorf("LooksUnbounded(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestT708ReapNeverSignalsItselfOrInit(t *testing.T) {
	// A pathological anchor that claims the daemon's own group. The reaper
	// must not be able to shoot the process that called it.
	tbl := Table{
		{PID: 1, PPID: 0, PGID: 1},
		{PID: 500, PPID: 1, PGID: 500, Command: "jevonsd"},
		{PID: 501, PPID: 500, PGID: 500, Command: "jevonsd child"},
	}
	var signalled []int
	tr := &Tracker{
		List:   func() (Table, error) { return tbl, nil },
		Signal: func(pid int, _ syscall.Signal) error { signalled = append(signalled, pid); return nil },
		Sleep:  func(time.Duration) {},
		Self:   501,
	}
	if _, err := tr.ReapAnchor(Anchor{Seat: "bad", PID: 500, PGID: 500}); err != nil {
		t.Fatalf("ReapAnchor: %v", err)
	}
	for _, pid := range signalled {
		if pid == 501 || pid == 500 || pid <= 1 {
			t.Fatalf("reaper signalled %d — its own process or an ancestor", pid)
		}
	}
}

func TestT708ReapRefusesAnImplausibleSelection(t *testing.T) {
	var tbl Table
	tbl = append(tbl, Proc{PID: 1, PPID: 0, PGID: 1}, Proc{PID: 900, PPID: 1, PGID: 900})
	for pid := 1000; pid < 1000+MaxReapSet+5; pid++ {
		tbl = append(tbl, Proc{PID: pid, PPID: 900, PGID: 900})
	}
	var signalled int
	tr := &Tracker{
		List:   func() (Table, error) { return tbl, nil },
		Signal: func(int, syscall.Signal) error { signalled++; return nil },
		Sleep:  func(time.Duration) {},
		Self:   7,
	}
	res, err := tr.ReapAnchor(Anchor{Seat: "runaway", PID: 900, PGID: 900})
	if err != nil {
		t.Fatalf("ReapAnchor: %v", err)
	}
	if signalled != 0 || res.Refused == "" {
		t.Fatalf("reaper signalled %d with refusal %q; a selection past the sanity cap must refuse", signalled, res.Refused)
	}
}

func TestT708TrackerReapWithoutAnchorIsNotAnError(t *testing.T) {
	tr := &Tracker{List: func() (Table, error) { return specimenTable(), nil }}
	res, err := tr.Reap("never-anchored")
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if res.Any() || res.Refused == "" {
		t.Fatalf("unanchored reap = %+v, want a refusal sentence and no signals", res)
	}
}

func TestT708ParseETime(t *testing.T) {
	cases := map[string]time.Duration{
		"00:05":      5 * time.Second,
		"40:12":      40*time.Minute + 12*time.Second,
		"01:02:03":   time.Hour + 2*time.Minute + 3*time.Second,
		"2-03:04:05": 2*24*time.Hour + 3*time.Hour + 4*time.Minute + 5*time.Second,
		"not-a-time": 0,
	}
	for in, want := range cases {
		if got := ParseETime(in); got != want {
			t.Errorf("ParseETime(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestT708ParsePSDropsUnparseableRows(t *testing.T) {
	out := "  900   500   900   3.0 01:00:00 codex --seat x\nrubbish\n  902     1   900 190.0 40:00 /bin/sh -c while :; do go test; done\n"
	tbl := ParsePS(out)
	if len(tbl) != 2 {
		t.Fatalf("parsed %d rows, want 2: %+v", len(tbl), tbl)
	}
	if tbl[1].PID != 902 || tbl[1].PGID != 900 || tbl[1].PPID != 1 {
		t.Fatalf("row = %+v", tbl[1])
	}
	if !LooksUnbounded(tbl[1].Command) {
		t.Fatalf("command %q lost its tail", tbl[1].Command)
	}
}

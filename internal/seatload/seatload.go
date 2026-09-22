// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatload bounds the background load a fleet seat creates and
// gives the capacity governor a lever over load that is already running
// (🎯T708).
//
// On 2026-09-20 a seat reproducing the 🎯T33 saturation condition ran
// `burn.sh`, which detaches 16 shells each looping `go test -race` with no
// timeout, no iteration cap and no stop condition. Four minutes in the host
// 1-minute load average was 127.76; forty minutes in it was 121.90 with 26
// shells, and the seat that created the load had gone `phase: idle` — its
// turn had ended, so nothing turn-scoped could ever reach those loops. Its
// parent was stopped and the overseer was stopped, so no running agent
// anywhere above the process group had a lever. The owner was paged for a
// `pkill`.
//
// Two distinct gaps, and this package is the first: nothing tied that
// process group to the seat. 🎯T165 reaps the agent; the loops are not its
// children in any sense the daemon tracked, so a reaped, idled or crashed
// seat left them running forever on the owner's machine.
//
// The fix is an anchor recorded while the seat's root process is alive.
// A pid alone is not enough: a detached child is reparented to pid 1 the
// moment its shell exits, so a walk of parent links from a dead root finds
// nothing. The process *group* survives the root, which is why the anchor
// records it and why the reap can still name descendants of a seat that is
// already gone.
//
// Everything that decides which processes belong to a seat is pure
// arithmetic over a Table, so the whole classifier can be driven from a
// synthetic process table; only the lister and the signaller touch the host.
package seatload

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Proc is one row of the host process table.
type Proc struct {
	PID  int
	PPID int
	// PGID is the process group. It is the load-bearing field: a detached
	// background child keeps its group when its parent dies, and that is
	// the only link back to the seat that survives the seat.
	PGID int
	// CPUPercent is the process's share of one core, as ps reports it.
	CPUPercent float64
	// Elapsed is wall-clock age.
	Elapsed time.Duration
	// Command is the full argv, for naming a load source to a human.
	Command string
}

// Table is a snapshot of the host process table.
type Table []Proc

// Anchor identifies a seat's process subtree in a way that outlives the
// seat's own root process.
type Anchor struct {
	Seat string
	PID  int
	PGID int
	At   time.Time
}

// Valid reports whether the anchor can be used to select processes. A zero
// or init-owned group is never a selector: reaping it would mean reaping
// the machine.
func (a Anchor) Valid() bool { return a.Seat != "" && (a.PID > 1 || a.PGID > 1) }

// byPID indexes a table.
func (t Table) byPID() map[int]Proc {
	m := make(map[int]Proc, len(t))
	for _, p := range t {
		m[p.PID] = p
	}
	return m
}

// Find returns the row for pid.
func (t Table) Find(pid int) (Proc, bool) {
	for _, p := range t {
		if p.PID == pid {
			return p, true
		}
	}
	return Proc{}, false
}

// AnchorFor reads pid's row and returns the anchor to record for seat. It
// fails when pid is not in the table — an anchor is only ever recorded from
// an observed live process, never from a number someone passed in.
func AnchorFor(t Table, seat string, pid int, at time.Time) (Anchor, error) {
	p, ok := t.Find(pid)
	if !ok {
		return Anchor{}, fmt.Errorf("seatload: no process %d to anchor seat %q to", pid, seat)
	}
	if p.PGID <= 1 {
		// A seat whose root sits in init's group cannot be told apart from
		// the rest of the machine. Anchor to the pid alone and say so.
		return Anchor{Seat: seat, PID: pid, At: at}, nil
	}
	return Anchor{Seat: seat, PID: pid, PGID: p.PGID, At: at}, nil
}

// Descendants returns every process the anchor owns, excluding the root
// itself: the transitive parent-link closure from the root, unioned with
// the anchor's process group.
//
// Both halves are needed and neither is redundant. The parent walk finds a
// child that changed group (a `setsid`-style escape is still visible while
// its parent lives); the group finds a child whose parent has exited and
// which the kernel reparented to pid 1, where the parent walk sees nothing.
// The 2026-09-20 loops were the second kind.
//
// Processes at or below pid 1 are never returned, and neither is any
// ancestor of the root: a bug in the selection must not be able to point
// the reaper at the daemon that called it.
func Descendants(t Table, a Anchor) []Proc {
	if !a.Valid() {
		return nil
	}
	index := t.byPID()
	ancestors := ancestorsOf(index, a.PID)
	owned := map[int]Proc{}

	// Parent-link closure from the root.
	children := map[int][]Proc{}
	for _, p := range t {
		children[p.PPID] = append(children[p.PPID], p)
	}
	queue := []int{a.PID}
	seen := map[int]bool{a.PID: true}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, c := range children[pid] {
			if seen[c.PID] {
				continue
			}
			seen[c.PID] = true
			owned[c.PID] = c
			queue = append(queue, c.PID)
		}
	}

	// Process-group membership — the half that survives the root.
	// Only a group this seat leads. A broker that starts several Cursor
	// seats in one group made every seat own every sibling's children
	// (2026-09-22: cl-t119 read as 184 processes while three agents
	// shared pgid 99272). The reparented-loop case is the seat's own
	// group: its pid and pgid are the same.
	if a.PGID > 1 && a.PGID == a.PID {
		for _, p := range t {
			if p.PGID == a.PGID && p.PID != a.PID {
				owned[p.PID] = p
			}
		}
	}

	out := make([]Proc, 0, len(owned))
	for pid, p := range owned {
		if pid <= 1 || ancestors[pid] {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// ancestorsOf returns the parent chain of pid, including pid itself.
func ancestorsOf(index map[int]Proc, pid int) map[int]bool {
	out := map[int]bool{}
	for hops := 0; pid > 0 && hops < 64; hops++ {
		if out[pid] {
			break
		}
		out[pid] = true
		p, ok := index[pid]
		if !ok {
			break
		}
		pid = p.PPID
	}
	return out
}

// Orphaned returns the descendants whose parent is no longer in the table —
// the ones a parent-link walk alone would miss. It exists so a report can
// say *why* the group reading was necessary rather than asserting it.
func Orphaned(t Table, procs []Proc) []Proc {
	index := t.byPID()
	var out []Proc
	for _, p := range procs {
		if _, ok := index[p.PPID]; !ok || p.PPID <= 1 {
			out = append(out, p)
		}
	}
	return out
}

// unboundedShapes are command shapes that describe a loop with no stop
// condition. The list is deliberately about the *shape* of the command, not
// about which program is being run: `go test` is not the problem, `while :;
// do go test; done` is.
var unboundedShapes = []string{
	"while :",
	"while true",
	"while [ 1 ]",
	"until false",
	"for((;;))",
	"for ((;;))",
	"repeat forever",
}

// boundedMarkers are the bounds that make such a loop acceptable. They
// count only when they wrap the loop — a bound *inside* the loop body
// bounds one iteration and nothing else. `burn.sh` ran
// `while :; do go test -race -count=1 ./...; done`: every iteration was
// scrupulously bounded and the loop ran for forty minutes.
var boundedMarkers = []string{
	"timeout ",
	"--deadline",
	"--max-iterations",
	"--for=",
	"ulimit -t",
}

// LooksUnbounded reports whether a command line describes work with no
// stop condition. It is a naming heuristic — what the governor tells a
// human about a load source — and never the sole reason to signal
// anything; the reaper acts on seat lifecycle, which is a fact.
func LooksUnbounded(command string) bool {
	c := strings.ToLower(command)
	loop := -1
	for _, s := range unboundedShapes {
		if i := strings.Index(c, s); i >= 0 && (loop < 0 || i < loop) {
			loop = i
		}
	}
	if loop < 0 {
		return false
	}
	for _, b := range boundedMarkers {
		if i := strings.Index(c, b); i >= 0 && i < loop {
			return false
		}
	}
	return true
}

// Source is one seat's contribution to host load, as the governor sees it.
type Source struct {
	Seat string
	Root int
	// Procs is how many processes the seat owns beyond its root.
	Procs int
	// CPUPercent is their summed share (100 == one saturated core).
	CPUPercent float64
	// Oldest is the age of the longest-running owned process.
	Oldest time.Duration
	// Unbounded is true when any owned command looks like a loop with no
	// stop condition.
	Unbounded bool
	// Orphaned is how many owned processes have lost their parent — the
	// ones no turn-scoped cleanup can ever reach.
	Orphaned int
	// Heaviest names the single worst command, for the sentence a human reads.
	Heaviest string
}

// Summarize folds a seat's owned processes into one Source.
func Summarize(t Table, a Anchor) Source {
	procs := Descendants(t, a)
	s := Source{Seat: a.Seat, Root: a.PID, Procs: len(procs), Orphaned: len(Orphaned(t, procs))}
	worst := 0.0
	for _, p := range procs {
		s.CPUPercent += p.CPUPercent
		if p.Elapsed > s.Oldest {
			s.Oldest = p.Elapsed
		}
		if LooksUnbounded(p.Command) {
			s.Unbounded = true
		}
		if p.CPUPercent >= worst {
			worst, s.Heaviest = p.CPUPercent, p.Command
		}
	}
	return s
}

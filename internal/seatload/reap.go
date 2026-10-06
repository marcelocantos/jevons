// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatload

import (
	"fmt"
	"sort"
	"sync"
	"syscall"
	"time"
)

// DefaultGrace is how long a seat's descendants get to exit on SIGTERM
// before the reaper escalates.
const DefaultGrace = 2 * time.Second

// MaxReapSet is the largest set of processes one seat's reap may signal.
// A selection bug on this path takes out the owner's machine, so the
// reaper refuses an implausible set rather than trusting its own arithmetic.
const MaxReapSet = 256

// Lister reads the host process table.
type Lister func() (Table, error)

// Signaller sends sig to pid. A negative pid targets the process group, as
// kill(2) defines it.
type Signaller func(pid int, sig syscall.Signal) error

// Result is what one reap did.
type Result struct {
	Seat string
	// Terminated are the pids that were signalled and are gone.
	Terminated []int
	// Survived are the pids still present after SIGKILL — reported, never
	// silently dropped.
	Survived []int
	// Orphaned is how many of the reaped processes had already lost their
	// parent, and so were unreachable from the seat's own turn.
	Orphaned int
	// Refused explains a reap that declined to signal anything.
	Refused string
}

// Any reports whether the reap had anything to do.
func (r Result) Any() bool { return len(r.Terminated) > 0 || len(r.Survived) > 0 }

// String is the one line the lifecycle log and the overseer read.
func (r Result) String() string {
	switch {
	case r.Refused != "":
		return fmt.Sprintf("seat %q: descendant reap refused: %s", r.Seat, r.Refused)
	case !r.Any():
		return fmt.Sprintf("seat %q: no surviving descendants", r.Seat)
	case len(r.Survived) > 0:
		return fmt.Sprintf("seat %q: terminated %d descendant process(es) (%d orphaned); %d survived SIGKILL: %v (🎯T708)",
			r.Seat, len(r.Terminated), r.Orphaned, len(r.Survived), r.Survived)
	default:
		return fmt.Sprintf("seat %q: terminated %d descendant process(es), %d of them orphaned and unreachable from the seat's turn (🎯T708)",
			r.Seat, len(r.Terminated), r.Orphaned)
	}
}

// Tracker records a live anchor per seat and reaps by it.
//
// The anchor is taken while the seat's root process is alive and kept after
// it dies, which is the whole point: at reap time the root may be gone and
// every loop it detached reparented to pid 1, and the recorded process group
// is then the only thing that still says which of them belonged to this seat.
type Tracker struct {
	// List reads the process table; nil uses the host lister.
	List Lister
	// Signal delivers a signal; nil uses syscall.Kill.
	Signal Signaller
	// Grace is the SIGTERM window; zero uses DefaultGrace.
	Grace time.Duration
	// Now and Sleep are the clock seams (tests).
	Now   func() time.Time
	Sleep func(time.Duration)
	// Self is the pid the reaper must never signal, nor any of its
	// ancestors; zero uses the running process.
	Self int

	mu      sync.Mutex
	anchors map[string]Anchor
}

// NewTracker returns a tracker wired to the host.
func NewTracker() *Tracker { return &Tracker{} }

func (t *Tracker) list() (Table, error) {
	if t.List != nil {
		return t.List()
	}
	return HostTable()
}

func (t *Tracker) signal(pid int, sig syscall.Signal) error {
	if t.Signal != nil {
		return t.Signal(pid, sig)
	}
	return hostSignal(pid, sig)
}

func (t *Tracker) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *Tracker) sleep(d time.Duration) {
	if t.Sleep != nil {
		t.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (t *Tracker) self() int {
	if t.Self != 0 {
		return t.Self
	}
	return hostPID()
}

// Track records the anchor for seat from pid's live row. Calling it again
// for the same seat replaces the anchor — a reminted seat is a new process.
func (t *Tracker) Track(seat string, pid int) (Anchor, error) {
	if t == nil {
		return Anchor{}, fmt.Errorf("seatload: nil tracker")
	}
	tbl, err := t.list()
	if err != nil {
		return Anchor{}, err
	}
	a, err := AnchorFor(tbl, seat, pid, t.now())
	if err != nil {
		return Anchor{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.anchors == nil {
		t.anchors = map[string]Anchor{}
	}
	t.anchors[seat] = a
	return a, nil
}

// Anchor returns the recorded anchor for seat.
func (t *Tracker) Anchor(seat string) (Anchor, bool) {
	if t == nil {
		return Anchor{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	a, ok := t.anchors[seat]
	return a, ok
}

// Forget drops seat's anchor without reaping — the seat handed its work off.
func (t *Tracker) Forget(seat string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.anchors, seat)
}

// Seats returns the seats currently anchored.
func (t *Tracker) Seats() []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.anchors))
	for s := range t.anchors {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// HostLoad summarises every anchored seat's current load.
func (t *Tracker) HostLoad() ([]Source, error) {
	if t == nil {
		return nil, nil
	}
	tbl, err := t.list()
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	anchors := make([]Anchor, 0, len(t.anchors))
	for _, a := range t.anchors {
		anchors = append(anchors, a)
	}
	t.mu.Unlock()
	sort.Slice(anchors, func(i, j int) bool { return anchors[i].Seat < anchors[j].Seat })
	out := make([]Source, 0, len(anchors))
	for _, a := range anchors {
		s := Summarize(tbl, a)
		if s.Procs == 0 {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// Reap terminates every process the seat owns and forgets the anchor. It is
// safe to call for a seat that was never anchored (nothing to do) and for a
// seat whose root is already dead — that is the case it exists for.
func (t *Tracker) Reap(seat string) (Result, error) {
	if t == nil {
		return Result{Seat: seat}, nil
	}
	a, ok := t.Anchor(seat)
	if !ok {
		return Result{Seat: seat, Refused: "no anchor recorded for this seat"}, nil
	}
	res, err := t.ReapAnchor(a)
	t.Forget(seat)
	return res, err
}

// otherAnchors returns every anchor this tracker holds other than
// excludeSeat, snapshotted under the lock.
func (t *Tracker) otherAnchors(excludeSeat string) []Anchor {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Anchor, 0, len(t.anchors))
	for seat, a := range t.anchors {
		if seat == excludeSeat {
			continue
		}
		out = append(out, a)
	}
	return out
}

// ReapAnchor terminates the processes an anchor owns.
//
// 🎯T844: the daemon (self, and self's ancestors) is not the only process a
// selection bug must never reach. On 2026-09-22 a seat's turn ran the daily
// activation bounce — the script that restarts jevonsd and the broker — and
// the relaunched fleet came up as process-tree descendants of that seat's
// own turn: the daemon, the broker's seat processes, and six sibling seats
// all hung off the reaped seat's root. A pure parent-link/group selection
// has no way to tell "my own detached loop" from "the fleet my turn happens
// to have bootstrapped" — both are genuine descendants. The fix is not a
// smarter walk; it is a second, independent fact the walk cannot see from
// inside one seat's own subtree: every OTHER anchor this tracker currently
// holds names a live, registered seat, and that seat's own process (and
// whatever Descendants finds for it) is never a valid target for this
// seat's reap, regardless of how it is connected in the process tree.
func (t *Tracker) ReapAnchor(a Anchor) (Result, error) {
	res := Result{Seat: a.Seat}
	if !a.Valid() {
		res.Refused = "anchor names no process group the machine can be told apart from"
		return res, nil
	}
	tbl, err := t.list()
	if err != nil {
		return res, err
	}
	procs := Descendants(tbl, a)
	if len(procs) == 0 {
		return res, nil
	}
	res.Orphaned = len(Orphaned(tbl, procs))

	self := t.self()
	guard := ancestorsOf(tbl.byPID(), self)
	// 🎯T844: no other registered seat's own process, nor anything that
	// seat's own anchor owns, is ever a target of this seat's reap — even
	// when it reads as a descendant of this seat's root (the activation-
	// bounce shape). Each other anchor is resolved against the SAME table
	// snapshot this reap is using, so the exclusion is self-consistent.
	for _, other := range t.otherAnchors(a.Seat) {
		if !other.Valid() {
			continue
		}
		guard[other.PID] = true
		for _, p := range Descendants(tbl, other) {
			guard[p.PID] = true
		}
	}
	var targets []int
	for _, p := range procs {
		if p.PID <= 1 || p.PID == self || guard[p.PID] {
			continue
		}
		targets = append(targets, p.PID)
	}
	if len(targets) == 0 {
		return res, nil
	}
	if len(targets) > MaxReapSet {
		res.Refused = fmt.Sprintf("selection of %d processes exceeds the %d-process sanity cap; refusing to signal", len(targets), MaxReapSet)
		return res, nil
	}

	for _, pid := range targets {
		_ = t.signal(pid, syscall.SIGTERM)
	}
	grace := t.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}
	t.sleep(grace)

	alive, err := t.livePIDs(targets)
	if err != nil {
		return res, err
	}
	for _, pid := range alive {
		_ = t.signal(pid, syscall.SIGKILL)
	}
	if len(alive) > 0 {
		t.sleep(grace)
		if left, lerr := t.livePIDs(alive); lerr == nil {
			res.Survived = left
		}
	}

	survived := map[int]bool{}
	for _, pid := range res.Survived {
		survived[pid] = true
	}
	for _, pid := range targets {
		if !survived[pid] {
			res.Terminated = append(res.Terminated, pid)
		}
	}
	return res, nil
}

// livePIDs re-reads the table and returns which of pids are still present.
func (t *Tracker) livePIDs(pids []int) ([]int, error) {
	tbl, err := t.list()
	if err != nil {
		return nil, err
	}
	index := tbl.byPID()
	var out []int
	for _, pid := range pids {
		if _, ok := index[pid]; ok {
			out = append(out, pid)
		}
	}
	return out, nil
}

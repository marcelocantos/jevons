// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Governor is the daemon-side seam over the pure policy: it holds the live
// in-flight counts, asks Plan for a verdict, and raises a single sticky owner
// notice when capacity has narrowed to owner work only.
//
// It owns no budget state of its own — Snapshot supplies that on every call —
// so the cost subsystem stays the single source of burn truth.
type Governor struct {
	snapshot func() Snapshot
	policy   func() *Policy
	notify   func(text string)
	now      func() time.Time

	mu       sync.Mutex
	running  map[Class]int
	last     map[Class]Decision
	lastAt   map[Class]time.Time
	assessed Assessment
	// noticed is the sticky-notice latch: the owner hears "background is
	// parked" once per narrowing, not once per deferred tick.
	noticed bool
	// Deferred / admitted counters make the status endpoint an honest
	// record of what the policy actually did.
	admitted map[Class]int
	deferred map[Class]int
}

// GovernorArgs constructs a Governor. Snapshot is required; the rest default.
type GovernorArgs struct {
	// Snapshot reports live budget and load. Required.
	Snapshot func() Snapshot
	// Policy reports the current (possibly retuned) policy. Nil uses the
	// shipped default.
	Policy func() *Policy
	// Notify delivers the sticky owner notice when only owner work fits, and
	// the all-clear when capacity recovers. Nil disables notices.
	Notify func(text string)
	// Now is injected for deterministic tests.
	Now func() time.Time
}

// NewGovernor constructs a Governor.
func NewGovernor(args GovernorArgs) *Governor {
	g := &Governor{
		snapshot: args.Snapshot,
		policy:   args.Policy,
		notify:   args.Notify,
		now:      args.Now,
		running:  map[Class]int{},
		last:     map[Class]Decision{},
		lastAt:   map[Class]time.Time{},
		admitted: map[Class]int{},
		deferred: map[Class]int{},
	}
	if g.now == nil {
		g.now = time.Now
	}
	if g.policy == nil {
		g.policy = DefaultPolicy
	}
	if g.snapshot == nil {
		g.snapshot = func() Snapshot { return Snapshot{} }
	}
	return g
}

// Begin asks for capacity to run one cycle of class (a free-form loop name is
// normalized). The returned release closure must be called when the cycle
// finishes; it is a no-op for work that was not admitted, so callers can
// always `defer release()`.
func (g *Governor) Begin(class, name string) (Decision, func()) {
	if g == nil {
		return Decision{Class: NormalizeClass(class), Name: name, Verdict: VerdictAdmit,
			Tier: TierFull, Reason: ReasonHeadroomOK, Detail: "no governor wired"}, func() {}
	}
	c := NormalizeClass(class)
	pol := g.policy()

	g.mu.Lock()
	snap := g.snapshot()
	snap.RunningBackground = g.runningCopyLocked()
	assessment := Assess(snap, pol)
	d := Admit(Request{Class: c, Name: name, Degradable: true}, snap, pol)
	g.assessed = assessment
	g.last[c] = d
	g.lastAt[c] = g.now()
	if d.Admitted() {
		g.running[c]++
		g.admitted[c]++
	} else {
		g.deferred[c]++
	}
	notice := g.noticeLocked(assessment, d)
	g.mu.Unlock()

	if notice != "" && g.notify != nil {
		g.notify(notice)
	}
	if !d.Admitted() {
		return d, func() {}
	}
	var once sync.Once
	return d, func() {
		once.Do(func() {
			g.mu.Lock()
			if g.running[c] > 0 {
				g.running[c]--
			}
			g.mu.Unlock()
		})
	}
}

// PlanQueue decides a whole queue against one snapshot, without reserving
// slots — the holistic view for the status API and for callers that schedule
// several cycles at once.
func (g *Governor) PlanQueue(queue []Request) ([]Decision, Assessment) {
	pol := g.policy()
	g.mu.Lock()
	snap := g.snapshot()
	snap.RunningBackground = g.runningCopyLocked()
	g.mu.Unlock()
	return Plan(queue, snap, pol), Assess(snap, pol)
}

// PreemptNow reports which in-flight background cycles should be cancelled
// under current pressure. The caller performs the cancellation — the governor
// never reaches into another loop's goroutine.
func (g *Governor) PreemptNow(running []Running) []Decision {
	pol := g.policy()
	g.mu.Lock()
	snap := g.snapshot()
	snap.RunningBackground = g.runningCopyLocked()
	g.mu.Unlock()
	return Preempt(running, snap, pol)
}

// runningCopyLocked snapshots the in-flight counters. Caller holds mu.
func (g *Governor) runningCopyLocked() map[Class]int {
	out := make(map[Class]int, len(g.running))
	for c, n := range g.running {
		if n > 0 {
			out[c] = n
		}
	}
	return out
}

// noticeLocked returns the owner notice to send, or "" for silence. Caller
// holds mu. The latch means a long stretch of critical pressure produces one
// notice and one all-clear, not a stream.
func (g *Governor) noticeLocked(a Assessment, d Decision) string {
	switch {
	case a.OwnerOnly && !g.noticed:
		g.noticed = true
		return fmt.Sprintf("Capacity notice: background fleet work is parked — %s. Owner turns and open Build missions still run. (last deferred: %s)",
			joinReasons(a.Reasons), strings.TrimSpace(string(d.Class)+" "+d.Name))
	case !a.OwnerOnly && g.noticed:
		g.noticed = false
		return fmt.Sprintf("Capacity notice cleared: headroom back to %.0f%% (pressure %s); background work resumes.",
			a.Headroom*100, a.Pressure)
	default:
		return ""
	}
}

// ClassStatus is one class's row in the status report.
type ClassStatus struct {
	Class    Class     `json:"class"`
	Rank     int       `json:"rank"`
	Running  int       `json:"running"`
	Admitted int       `json:"admitted"`
	Deferred int       `json:"deferred"`
	Last     *Decision `json:"last,omitempty"`
	LastAt   string    `json:"last_at,omitempty"`
}

// Status is the whole admission picture, for GET /api/capacity and the MCP
// status tool.
type Status struct {
	At         string        `json:"at"`
	Assessment Assessment    `json:"assessment"`
	Snapshot   Snapshot      `json:"snapshot"`
	Policy     *Policy       `json:"policy"`
	Classes    []ClassStatus `json:"classes"`
	// Plan is what each background class would get if it asked right now.
	Plan []Decision `json:"plan"`
}

// Status computes the current admission picture without reserving anything.
func (g *Governor) Status() Status {
	pol := g.policy()
	g.mu.Lock()
	snap := g.snapshot()
	snap.RunningBackground = g.runningCopyLocked()
	rows := make([]ClassStatus, 0, len(ranked))
	for _, c := range ranked {
		row := ClassStatus{Class: c, Rank: Rank(c), Running: g.running[c],
			Admitted: g.admitted[c], Deferred: g.deferred[c]}
		if d, ok := g.last[c]; ok {
			dd := d
			row.Last = &dd
			row.LastAt = g.lastAt[c].UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}
	now := g.now()
	g.mu.Unlock()

	queue := make([]Request, 0, len(ranked))
	for _, c := range ranked {
		if c.Background() {
			queue = append(queue, Request{Class: c, Name: "hypothetical", Degradable: true})
		}
	}
	return Status{
		At:         now.UTC().Format(time.RFC3339),
		Assessment: Assess(snap, pol),
		Snapshot:   snap,
		Policy:     pol,
		Classes:    rows,
		Plan:       Plan(queue, snap, pol),
	}
}

// Gate is the interface ambient loops depend on (Governor implements it):
// ask before each tick, release when the pass ends. Loops take a Gate rather
// than a *Governor so tests can admit or defer deterministically.
//
// A nil Gate means "no admission control wired" — the loop runs, exactly as
// it did before 🎯T359.
type Gate interface {
	Begin(class, name string) (Decision, func())
}

// Admit is the nil-safe helper a loop calls at the top of a tick:
//
//	d, release := capacity.Ask(gate, capacity.ClassResearch, trigger)
//	defer release()
//	if !d.Admitted() { ...record the skip and return... }
func Ask(g Gate, class Class, name string) (Decision, func()) {
	if g == nil {
		return Decision{Class: class, Name: name, Verdict: VerdictAdmit, Tier: TierFull,
			Reason: ReasonHeadroomOK, Detail: "no capacity gate wired"}, func() {}
	}
	return g.Begin(string(class), name)
}

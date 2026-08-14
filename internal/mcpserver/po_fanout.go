// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/pofanout"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/staffops"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T380: a product owner cannot sit idle on a non-empty frontier without the
// overseer being told why. The doctrine (🎯T155 / 🎯T193 / 🎯T325.1 / 🎯T111.4)
// is thorough and entirely instructional, so a PO that answers a fan-out order
// with silence is indistinguishable in agent_list from one correctly asleep on
// an all-gated frontier. This file is the sentinel's half of the reading: it
// projects registry + activity-tracker + cross-cycle turn bookkeeping into
// [pofanout.POObs], and hands the faults to staffops as signals the overseer
// sees. The classification itself is the pure package; nothing here decides.

// poFanoutState is the per-PO bookkeeping the fault needs and no single sample
// can see: whether a turn ran to completion since the last cycle, and how many
// work children the PO had when that turn began.
type poFanoutState struct {
	// lastPhase is the activity phase observed on the previous cycle.
	lastPhase string
	// childrenAtTurnStart is the live work-child count when the current (or
	// most recently completed) turn began.
	childrenAtTurnStart int
	// turnEnded is true once a turn has completed and the PO went idle again,
	// and stays true until the next turn begins — the sentinel samples on an
	// interval, so the transition must outlive the cycle that saw it.
	turnEnded bool
}

// poFanoutObserved is one PO projected from the live surfaces, before
// classification.
type poFanoutObserved struct {
	obs      pofanout.POObs
	workdir  string
	targetID string
}

// samplePOFanout reads every product owner scoped to this ledger against the
// same frontier leaves the stall check uses, and returns the faults.
//
// leaves must already carry AlreadyEngaged / ForceEngage as the frontier block
// computed them: legitimate sleep is exactly poproactive.Sleep over that set,
// which is what keeps this off agent-name heuristics for the gating decision.
func (s *Server) samplePOFanout(rt *sentinelRuntime, leaves []poproactive.LeafObs, overseer, workdir string, now time.Time, grace time.Duration, running func(string) bool) []staffops.POFanoutObs {
	if s == nil || s.registry == nil || rt == nil {
		return nil
	}
	if running == nil {
		running = func(name string) bool {
			proc := s.registry.Get(name)
			return proc != nil && proc.Alive()
		}
	}

	defs := s.registry.List()
	wantLedger := targetfile.LedgerKey(workdir)

	var observed []poFanoutObserved
	for _, d := range defs {
		name := strings.TrimSpace(d.Name)
		if name == "" || name == overseer || !isPOName(name) {
			continue
		}
		// Product scope: a PO on another repo's ledger is not answerable for
		// this frontier (🎯T200 portfolios put several POs in one registry).
		// Unknown is a match, not a mismatch — see targetfile.SameLedger: a
		// registry row with no workdir keeps its pre-🎯T389 visibility rather
		// than becoming a PO no sentinel can ever fault.
		if !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		alive := running(name)
		phase := ""
		if s.idleActivity != nil {
			phase = s.idleActivity.Get(name).Phase
		}
		children := liveWorkChildren(defs, name, running)

		po := pofanout.POObs{
			Name:             name,
			Alive:            alive,
			Phase:            phase,
			LiveWorkChildren: children,
		}
		po.TurnEnded, po.NewChildrenThisTurn, po.GraceElapsed = s.trackPOFanoutTurn(rt, name, phase, alive, children, now, grace)
		observed = append(observed, poFanoutObserved{obs: po, workdir: d.WorkDir, targetID: d.TargetID})
	}
	if len(observed) == 0 {
		return nil
	}

	pos := make([]pofanout.POObs, 0, len(observed))
	for _, o := range observed {
		pos = append(pos, o.obs)
	}
	faults := pofanout.Faults(pofanout.ClassifyAll(pos, leaves))
	if len(faults) == 0 {
		return nil
	}

	out := make([]staffops.POFanoutObs, 0, len(faults))
	for _, f := range faults {
		out = append(out, staffops.POFanoutObs{
			Name:       f.Name,
			Verdict:    string(f.Verdict),
			Reason:     f.Reason,
			ReadyCount: len(f.ReadyIDs),
			Detail:     f.Detail,
		})
		// Durable record beside the wire report: the overseer is told now, and
		// the eventlog still holds the evidence when the report has scrolled.
		s.logLifecycle(compSentinel, "po_fanout", "error", map[string]any{
			"po":      f.Name,
			"verdict": string(f.Verdict),
			"reason":  f.Reason,
			"ready":   len(f.ReadyIDs),
			"detail":  f.Detail,
		})
	}
	return out
}

// trackPOFanoutTurn advances the cross-cycle bookkeeping for one PO and reports
// what this cycle can say about its last turn and how long it has been idle.
//
// A turn is observed as a phase excursion away from idle and back: the delivery
// of an overseer fan-out order is exactly such an excursion, so an order that
// ended in silence is visible here without instrumenting the send path.
func (s *Server) trackPOFanoutTurn(rt *sentinelRuntime, name, phase string, alive bool, children int, now time.Time, grace time.Duration) (turnEnded bool, newChildren int, graceElapsed bool) {
	idle := poFanoutPhaseIdle(phase)

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.poFanout == nil {
		rt.poFanout = make(map[string]poFanoutState)
	}
	st := rt.poFanout[name]

	switch {
	case !alive:
		// A dead PO carries nothing forward — rehydration starts a fresh read.
		delete(rt.poFanout, name)
		delete(rt.firstSeen, poFanoutSymptom(name))
		return false, 0, false
	case !idle:
		// Turn in flight. If it just began, snapshot the child count so any
		// spawn it performs is attributable to this turn.
		if poFanoutPhaseIdle(st.lastPhase) || st.lastPhase == "" {
			st.childrenAtTurnStart = children
		}
		st.turnEnded = false
		st.lastPhase = phase
		rt.poFanout[name] = st
		delete(rt.firstSeen, poFanoutSymptom(name))
		return false, 0, false
	default:
		// Idle. A prior non-idle phase means the turn has just completed.
		if !poFanoutPhaseIdle(st.lastPhase) && st.lastPhase != "" {
			st.turnEnded = true
		}
		st.lastPhase = phase
		rt.poFanout[name] = st
	}

	if st.turnEnded {
		if newChildren = children - st.childrenAtTurnStart; newChildren < 0 {
			newChildren = 0
		}
	}

	sym := poFanoutSymptom(name)
	first, seen := rt.firstSeen[sym]
	if !seen {
		rt.firstSeen[sym] = now
		first = now
	}
	return st.turnEnded, newChildren, now.Sub(first) >= grace
}

// poFanoutSymptom is the firstSeen key for a PO's idle-grace bookkeeping.
func poFanoutSymptom(name string) string { return "po_fanout:" + name }

// poFanoutPhaseIdle mirrors pofanout's reading of a phase string: unknown
// counts as idle, as it does for the idle-nudge sweep.
func poFanoutPhaseIdle(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "", "idle":
		return true
	default:
		return false
	}
}

// liveWorkChildren counts the live work agents parented by name.
func liveWorkChildren(defs []claudia.AgentDef, name string, running func(string) bool) int {
	n := 0
	for _, d := range defs {
		if strings.TrimSpace(d.Parent) != name {
			continue
		}
		purpose := strings.TrimSpace(d.Purpose)
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		if purpose != claudia.PurposeWork {
			continue
		}
		if running(d.Name) {
			n++
		}
	}
	return n
}

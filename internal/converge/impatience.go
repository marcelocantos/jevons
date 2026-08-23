// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package converge holds the pure convergence-impatience policy (🎯T317):
// when an open-mission gap stays unsatisfied, escalate *noise* on a bounded
// timeline — re-pressure (🎯T315 actuator), then elevated noise to the root
// overseer, then a human-visible sticky alert.
//
// Two rules govern the whole ladder:
//
//   - Noise is not satisfaction. Firing any rung — including the overseer
//     event — never marks the gap satisfied and never removes it from the
//     reconcile set (🎯T316 owns satisfaction semantics; this package only
//     consumes it via Gap.Satisfied / set membership).
//   - The human rung is not an ack-only latch (🎯T319 owner correction).
//     True satisfaction clears the sticky on its own; a human ack may
//     dismiss early but is never required to clear.
package converge

import (
	"sort"
	"time"

	"github.com/marcelocantos/jevons/internal/converge/attenuate"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// Rung is one step on the escalation ladder, ordered by loudness.
type Rung int

const (
	RungNone Rung = iota
	// RungRepressure re-delivers the mission to the idle agent (🎯T315).
	RungRepressure
	// RungOverseerNoise pushes a converge-impatient/-failing event to the
	// root overseer. A noise step: it informs, it does not satisfy.
	RungOverseerNoise
	// RungHumanAlert raises owner-visible sticky chrome (daily chat and/or
	// macOS notification).
	RungHumanAlert
)

func (r Rung) String() string {
	switch r {
	case RungRepressure:
		return "repressure"
	case RungOverseerNoise:
		return "overseer-noise"
	case RungHumanAlert:
		return "human-alert"
	}
	return "none"
}

// Documented timeline constants (acceptance 1). Dwell is measured from the
// moment the gap entered the reconcile set, not from the last rung.
const (
	// RepressureAfter is the grace period before the first actuator fires:
	// long enough that a normally-starting agent is never nudged.
	RepressureAfter = 4 * time.Minute
	// OverseerNoiseAfter is when re-pressure is judged to have failed and
	// the root overseer must hear about it.
	OverseerNoiseAfter = 15 * time.Minute
	// HumanAlertAfter is when overseer-level noise has also failed to
	// produce satisfaction and the owner must see it.
	HumanAlertAfter = 45 * time.Minute
)

// Anti-thrash: minimum spacing between repeats of the same rung for one
// agent (acceptance 4). A tick that finds several rungs due fires only the
// loudest one, so a fast converge loop cannot spam per tick.
const (
	RepressureEvery    = 5 * time.Minute
	OverseerNoiseEvery = 20 * time.Minute
	HumanAlertEvery    = 30 * time.Minute
)

// Gap is the minimal view this ladder consumes from the 🎯T316 reconcile
// set: one open-mission gap that is not yet satisfied. T316 owns what
// "satisfied" means (agent phase=working, or mission closed/reaped); the
// ladder only reads the verdict.
//
// A gap that disappears from the set between ticks is treated exactly like
// Satisfied: the plant is healthy and the noise must stop.
type Gap struct {
	Agent   string
	Mission string
	// Since is when the gap entered the set (start of dwell).
	Since time.Time
	// Satisfied is T316's verdict. Satisfied gaps may still be passed in;
	// the ladder uses them to clear noise and close the incident.
	Satisfied bool
	// FleetIntent / Intent are the 🎯T414 deliberate states. Empty resolves
	// to working, so a gap set assembled without them ladders exactly as it
	// did before intent existed.
	//
	// A gap under a non-working intent is not a gap: nothing is failing to
	// happen that anyone wanted to happen. On 2026-08-10 this ladder fired
	// repressure twice at each of three parked workers and then closed the
	// incidents as cleared, which is the worst of both — it acted, and then
	// it reported that the situation had resolved.
	FleetIntent fleetintent.State
	Intent      fleetintent.State

	// RefusalOnly is 🎯T454: the agent's turns are producing only provider
	// refusals. The ladder holds the incident open and suppresses further
	// rungs (composes with 🎯T406 blocked_provider rather than duplicating
	// it — either signal is enough to stop spending failed API calls).
	RefusalOnly bool
	// Cause is set on a Satisfied gap when the close reason is known
	// (🎯T454 clause 2). Zero means ClosedBySatisfaction.
	Cause CloseCause
}

// Runnable reports whether this gap's intent says the agent should be
// running. A gap whose intent declines is held, not fired at and not
// silently satisfied.
func (g Gap) Runnable() bool {
	return fleetintent.Allows(g.FleetIntent, g.Intent, fleetintent.ControlRepressure).Allow
}

// ActionKind is what the caller must actuate for one Action.
type ActionKind int

const (
	// ActFire raises the Action's Rung (re-pressure, overseer event, or
	// human sticky). Never satisfies the gap.
	ActFire ActionKind = iota
	// ActClearHuman withdraws owner-visible sticky chrome because the gap
	// reached true satisfaction (🎯T319). Emitted only if the human rung
	// had been raised.
	ActClearHuman
)

// Action is one actuation for one agent on one tick.
type Action struct {
	Kind    ActionKind
	Agent   string
	Mission string
	Rung    Rung
	// Class is the event class for a rung that pushes an event to the root
	// overseer: EventClassImpatient the first time, EventClassFailing once
	// the same gap has already been shouted about. Empty for other rungs.
	Class string
	// Dwell is how long the gap had been unsatisfied when this fired.
	Dwell time.Duration
	// Satisfies is always false: no rung on this ladder is satisfaction
	// (acceptance 2). Present so callers cannot mistake a fired action for
	// gap closure.
	Satisfies bool
	Reason    string
}

// Incident is the per-agent record of one impatience episode: it opens when
// the first rung fires and closes when the gap reaches satisfaction or
// leaves the set. Closed incidents are handed to 🎯T319, which owns the
// root-delivered mini-postmortem; this package neither formats nor delivers
// it, and closing an incident is a report step, not satisfaction.
type Incident struct {
	Agent    string
	Mission  string
	Opened   time.Time
	Closed   time.Time
	Dwell    time.Duration
	Rungs    []Rung
	HumanLit bool
	// Acked is true if the owner dismissed the sticky before satisfaction.
	Acked bool
	// Cause is how the episode ended: a satisfaction verdict from 🎯T316, or
	// the gap departing the reconcile set. Both are healthy; they tell the
	// owner different stories, so the postmortem (🎯T319) renders them apart.
	Cause CloseCause
}

// agentState is the ladder's memory for one agent across ticks.
type agentState struct {
	mission  string
	since    time.Time
	lastFire map[Rung]time.Time
	rungs    []Rung
	humanLit bool
	acked    bool
}

// Ladder holds escalation state across reconcile ticks. Not safe for
// concurrent use; the converge loop drives it from one goroutine.
type Ladder struct {
	agents map[string]*agentState
	// atten is the optional 🎯T318 progress-attenuation policy. Nil means the
	// ladder runs at full timing. See attenuation.go.
	atten *attenuate.Attenuator
}

// NewLadder returns an empty ladder.
func NewLadder() *Ladder {
	return &Ladder{agents: map[string]*agentState{}}
}

// Tracked reports whether the ladder still considers this agent's gap open.
// Firing any rung leaves it tracked — the proof that noise is not
// satisfaction (acceptance 2).
func (l *Ladder) Tracked(agent string) bool {
	_, ok := l.agents[agent]
	return ok
}

// Ack records an owner dismissal of the human sticky for agent. It silences
// further human-rung noise but does not clear the gap, does not stop the
// lower rungs, and does not close the incident (🎯T319 (a)).
func (l *Ladder) Ack(agent string) {
	if st, ok := l.agents[agent]; ok {
		st.acked = true
		st.humanLit = false
	}
}

// Reconcile advances the ladder one tick over the current 🎯T316 reconcile
// set and returns the actions to actuate plus the incidents that closed on
// this tick. Gaps absent from set are treated as satisfied.
//
// At most one Action per agent per tick.
func (l *Ladder) Reconcile(now time.Time, set []Gap) ([]Action, []Incident) {
	present := make(map[string]bool, len(set))
	var actions []Action

	for _, g := range set {
		if g.Agent == "" {
			continue
		}
		present[g.Agent] = true
		if g.Satisfied {
			continue
		}
		// 🎯T414: intent declines the rung. Note that the gap stays present —
		// a held gap must not read as a departure, because departure closes
		// the incident and closing it says the situation resolved. Nothing
		// resolved: the agent is parked, and the gap is exactly where it was
		// when intent returns to working.
		if !g.Runnable() {
			continue
		}
		// 🎯T454: refusal-only turns — stop climbing. Repressure / overseer
		// noise / human-alert against a wall burn failed API calls and then
		// false-clear when the account recovers.
		if g.RefusalOnly {
			continue
		}
		st, ok := l.agents[g.Agent]
		if !ok {
			st = &agentState{since: g.Since, lastFire: map[Rung]time.Time{}}
			l.agents[g.Agent] = st
		}
		st.mission = g.Mission
		if !g.Since.IsZero() && g.Since.Before(st.since) {
			st.since = g.Since
		}
		dwell := now.Sub(st.since)
		// 🎯T318: visible progress shifts the thresholds out and caps how loud
		// the ladder may get. It never removes the gap from the set.
		effective, ceiling := l.attenuated(g.Agent, dwell, now)
		rung := dueRung(effective, now, st, ceiling)
		if rung == RungNone {
			continue
		}
		st.lastFire[rung] = now
		st.rungs = append(st.rungs, rung)
		if rung == RungHumanAlert {
			st.humanLit = true
		}
		actions = append(actions, Action{
			Kind:    ActFire,
			Agent:   g.Agent,
			Mission: g.Mission,
			Rung:    rung,
			Class:   eventClass(rung, st),
			Dwell:   dwell,
			Reason:  reasonFor(rung),
		})
	}

	// Satisfaction (verdict or departure from the set) clears noise and
	// closes the incident — no human ack required (🎯T319).
	var closed []Incident
	for agent, st := range l.agents {
		if present[agent] && !satisfiedIn(set, agent) {
			continue
		}
		if st.humanLit {
			actions = append(actions, Action{
				Kind:    ActClearHuman,
				Agent:   agent,
				Mission: st.mission,
				Rung:    RungHumanAlert,
				Dwell:   now.Sub(st.since),
				Reason:  "gap reached satisfaction; no stuck evidence remains",
			})
		}
		if len(st.rungs) > 0 {
			cause := ClosedByDeparture
			if satisfiedIn(set, agent) {
				cause = closeCauseIn(set, agent)
			}
			closed = append(closed, Incident{
				Agent:    agent,
				Mission:  st.mission,
				Opened:   st.since,
				Closed:   now,
				Dwell:    now.Sub(st.since),
				Rungs:    append([]Rung(nil), st.rungs...),
				HumanLit: st.humanLit,
				Acked:    st.acked,
				Cause:    cause,
			})
		}
		// 🎯T318: attenuation state dies with the gap, and only with the gap.
		l.forgetAttenuation(agent)
		delete(l.agents, agent)
	}

	sort.Slice(actions, func(i, j int) bool { return actions[i].Agent < actions[j].Agent })
	sort.Slice(closed, func(i, j int) bool { return closed[i].Agent < closed[j].Agent })
	return actions, closed
}

// dueRung picks the loudest rung at or below ceiling whose dwell threshold has
// passed and whose anti-thrash interval has elapsed (acceptance 4).
//
// ceiling is 🎯T318's attenuation cap. It is an input to the choice rather
// than a clamp on the result: a rung the ceiling excludes is skipped, and a
// quieter rung is chosen only if it is itself due and past its own interval.
// Clamping afterwards would fire the quieter rung out of turn and thrash.
func dueRung(dwell time.Duration, now time.Time, st *agentState, ceiling Rung) Rung {
	type step struct {
		rung  Rung
		after time.Duration
		every time.Duration
	}
	for _, s := range []step{
		{RungHumanAlert, HumanAlertAfter, HumanAlertEvery},
		{RungOverseerNoise, OverseerNoiseAfter, OverseerNoiseEvery},
		{RungRepressure, RepressureAfter, RepressureEvery},
	} {
		if s.rung > ceiling {
			continue
		}
		if dwell < s.after {
			continue
		}
		if s.rung == RungHumanAlert && st.acked {
			continue
		}
		if last, ok := st.lastFire[s.rung]; ok && now.Sub(last) < s.every {
			continue
		}
		return s.rung
	}
	return RungNone
}

func satisfiedIn(set []Gap, agent string) bool {
	for _, g := range set {
		if g.Agent == agent {
			return g.Satisfied
		}
	}
	return false
}

// closeCauseIn returns the Satisfied gap's Cause. ProviderResume is the only
// non-default stamp; everything else that satisfiedIn reports is agent work.
func closeCauseIn(set []Gap, agent string) CloseCause {
	for _, g := range set {
		if g.Agent == agent && g.Satisfied {
			if g.Cause == ClosedByProviderResume {
				return ClosedByProviderResume
			}
			return ClosedBySatisfaction
		}
	}
	return ClosedBySatisfaction
}

func reasonFor(r Rung) string {
	switch r {
	case RungRepressure:
		return "open mission idle past grace; re-pressuring the agent"
	case RungOverseerNoise:
		return "re-pressure failed to produce work; notifying root overseer (noise, not satisfaction)"
	case RungHumanAlert:
		return "overseer noise failed to produce work; owner-visible alert"
	}
	return ""
}

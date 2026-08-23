// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"sort"
	"sync"
	"time"
)

// DefaultMinStepInterval is the floor between two step attempts on one gap
// when the caller supplies no due policy. It matches the first idle-nudge
// backoff so the standing set does not thrash a worker mid-turn.
const DefaultMinStepInterval = 2 * time.Minute

// Resolution is what one Reconcile call did to the standing set.
type Resolution string

const (
	// ResolutionOpened: a new gap entered the set (or a re-bound agent
	// started a new episode).
	ResolutionOpened Resolution = "opened"
	// ResolutionPersisting: the gap was already open and remains unsatisfied.
	ResolutionPersisting Resolution = "persisting"
	// ResolutionSatisfied: the desired state now holds; the gap left the set.
	ResolutionSatisfied Resolution = "satisfied"
	// ResolutionWithdrawn: the agent was scoped out (deliberate stop,
	// design-gated, no open mission, not a work agent). Not an achievement.
	ResolutionWithdrawn Resolution = "withdrawn"
	// ResolutionNone: nothing was tracked and nothing needed tracking.
	ResolutionNone Resolution = "none"
)

// Outcome is the result of reconciling one observation against the set.
type Outcome struct {
	Key        GapKey
	Resolution Resolution
	Condition  Condition
	Reason     string
	// Gap is the gap as it now stands (zero-valued when it left the set).
	Gap MissionGap
	// ResidualMissionOpen flags satisfaction that closed the *agent's* gap
	// while the mission itself is still wanted — the reap-with-open-target
	// case. The successor gap belongs to whoever respawns; the set does not
	// invent an owner for it.
	ResidualMissionOpen bool
}

// Set is the standing reconcile set: every open-mission gap the engine is
// still impatient about. It is safe for concurrent use.
//
// The contract that makes this 🎯T316 rather than another nudge loop:
// Reconcile is the ONLY method that can remove a gap, and it removes one only
// on an Observation that classifies as satisfied or out-of-scope. RecordStep
// and Drive can add history and move timers; they can never empty the set.
type Set struct {
	mu       sync.Mutex
	gaps     map[GapKey]*MissionGap
	episodes map[GapKey]int
}

// NewSet returns an empty reconcile set.
func NewSet() *Set {
	return &Set{gaps: map[GapKey]*MissionGap{}, episodes: map[GapKey]int{}}
}

// Reconcile folds one observation into the set and reports what happened.
func (s *Set) Reconcile(o Observation, now time.Time) Outcome {
	key := KeyFor(o.Name)
	cond, kind, reason := ClassifyObservation(o)

	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" {
		return Outcome{Resolution: ResolutionNone, Condition: cond, Reason: "empty_agent_name"}
	}
	existing := s.gaps[key]

	switch cond {
	case ConditionSatisfied:
		out := Outcome{Key: key, Condition: cond, Reason: reason, Resolution: ResolutionNone}
		if existing != nil {
			delete(s.gaps, key)
			out.Resolution = ResolutionSatisfied
			out.Gap = existing.clone()
		}
		// A reap with the mission still open closes this agent's gap and
		// leaves the desired state wanting.
		out.ResidualMissionOpen = reason == "agent_reaped" && o.MissionOpen
		return out

	case ConditionOutOfScope:
		out := Outcome{Key: key, Condition: cond, Reason: reason, Resolution: ResolutionNone}
		if existing != nil {
			delete(s.gaps, key)
			out.Resolution = ResolutionWithdrawn
			out.Gap = existing.clone()
		}
		return out
	}

	// ConditionGap: open or keep open.
	mission := o.TargetID
	if existing != nil && existing.Mission == mission {
		existing.Kind = kind
		existing.Reason = reason
		existing.LastSeen = now
		return Outcome{
			Key:        key,
			Condition:  cond,
			Reason:     reason,
			Resolution: ResolutionPersisting,
			Gap:        existing.clone(),
		}
	}
	// New gap, or the agent was re-bound to a different mission: new episode.
	s.episodes[key]++
	g := &MissionGap{
		Key:         key,
		Agent:       o.Name,
		Mission:     mission,
		Kind:        kind,
		Reason:      reason,
		OpenedAt:    now,
		LastSeen:    now,
		Episode:     s.episodes[key],
		StepsByKind: map[StepKind]int{},
	}
	s.gaps[key] = g
	return Outcome{Key: key, Condition: cond, Reason: reason, Resolution: ResolutionOpened, Gap: g.clone()}
}

// RecordStep appends a step attempt to an open gap and returns the updated
// gap. The gap stays in the set: steps are progress toward satisfaction, never
// satisfaction. Recording against an unknown key is a no-op (false).
func (s *Set) RecordStep(key GapKey, rec StepRecord) (MissionGap, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.gaps[key]
	if g == nil {
		return MissionGap{}, false
	}
	rec.Key = key
	g.Steps = append(g.Steps, rec)
	if g.StepsByKind == nil {
		g.StepsByKind = map[StepKind]int{}
	}
	g.StepsByKind[rec.Kind]++
	if rec.At.After(g.LastStepAt) {
		g.LastStepAt = rec.At
	}
	return g.clone(), true
}

// Open returns every standing gap, oldest first. The engine stays persistently
// aware of exactly this list.
func (s *Set) Open() []MissionGap {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]MissionGap, 0, len(s.gaps))
	for _, g := range s.gaps {
		out = append(out, g.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].OpenedAt.Equal(out[j].OpenedAt) {
			return out[i].OpenedAt.Before(out[j].OpenedAt)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Get returns one standing gap.
func (s *Set) Get(key GapKey) (MissionGap, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.gaps[key]
	if g == nil {
		return MissionGap{}, false
	}
	return g.clone(), true
}

// Len is the number of standing gaps.
func (s *Set) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.gaps)
}

// DuePolicy decides whether a standing gap may take another step now. 🎯T317
// supplies the real ladder (rung per age and step history); 🎯T318 attenuates
// it on visible progress. DefaultDue is the floor used when neither is wired.
type DuePolicy func(g MissionGap, now time.Time) bool

// DefaultDue allows a step when the gap has never been stepped, or when the
// minimum interval has elapsed since the last attempt.
func DefaultDue(min time.Duration) DuePolicy {
	if min <= 0 {
		min = DefaultMinStepInterval
	}
	return func(g MissionGap, now time.Time) bool { return g.SinceLastStep(now) >= min }
}

// Actuator applies one step toward closing a gap. This is the whole integration
// surface: 🎯T315 implements re-pressure/rehydrate, 🎯T317 implements the noise
// rungs. An actuator returning an empty StepKind and no error means "nothing to
// do this tick" and is not recorded.
//
// An actuator can never satisfy a gap — Drive records what it did and leaves
// the gap standing. Only the next Observation decides.
type Actuator interface {
	Step(g MissionGap, now time.Time) (StepKind, error)
}

// ActuatorFunc adapts a function to Actuator.
type ActuatorFunc func(g MissionGap, now time.Time) (StepKind, error)

// Step implements Actuator.
func (f ActuatorFunc) Step(g MissionGap, now time.Time) (StepKind, error) { return f(g, now) }

// Drive offers every standing gap to the actuator, subject to the due policy,
// and records what came back. The set is unchanged in membership: Drive can
// only add history.
func (s *Set) Drive(now time.Time, due DuePolicy, act Actuator) []StepRecord {
	if act == nil {
		return nil
	}
	if due == nil {
		due = DefaultDue(DefaultMinStepInterval)
	}
	var out []StepRecord
	for _, g := range s.Open() {
		if !due(g, now) {
			continue
		}
		kind, err := act.Step(g, now)
		if kind == "" && err == nil {
			continue
		}
		rec := StepRecord{Key: g.Key, Kind: kind, At: now, Delivered: err == nil}
		if err != nil {
			rec.Err = err.Error()
		}
		if _, ok := s.RecordStep(g.Key, rec); ok {
			out = append(out, rec)
		}
	}
	return out
}

// --- bridge to the 🎯T317 escalation ladder -------------------------------
//
// impatience.go consumes a deliberately minimal Gap view {Agent, Mission,
// Since, Satisfied}. This package owns what those fields mean: Since is when
// the gap entered the standing set, and Satisfied is only ever the verdict of
// an Observation — never a consequence of a rung having fired.

// LadderView projects a standing gap into the ladder's consumption type.
// Satisfied is false by construction: a gap in the set is unsatisfied.
func (g MissionGap) LadderView() Gap {
	return Gap{Agent: g.Agent, Mission: g.Mission, Since: g.OpenedAt}
}

// LadderGaps is the whole standing set in the ladder's view, oldest first.
// A gap that reached satisfaction simply stops appearing here, which the
// ladder reads as satisfaction and uses to clear human noise (🎯T319).
func (s *Set) LadderGaps() []Gap {
	open := s.Open()
	out := make([]Gap, 0, len(open))
	for _, g := range open {
		out = append(out, g.LadderView())
	}
	return out
}

// LadderView reports the satisfaction verdict for a gap that left the set on
// this tick, so the ladder can close the incident in the same pass instead of
// inferring it from absence. Withdrawal (scoped out) reports false: the gap is
// gone, but nothing was achieved and no postmortem is owed.
func (o Outcome) LadderView() (Gap, bool) {
	if o.Resolution != ResolutionSatisfied {
		return Gap{}, false
	}
	cause := ClosedBySatisfaction
	if o.Reason == "provider_resumed_service" {
		cause = ClosedByProviderResume
	}
	return Gap{
		Agent:     o.Gap.Agent,
		Mission:   o.Gap.Mission,
		Since:     o.Gap.OpenedAt,
		Satisfied: true,
		Cause:     cause,
	}, true
}

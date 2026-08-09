// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package converge models convergence impatience (🎯T316): an open mission
// whose agent is not working is a persistent desired-state gap, and it stays
// in the reconcile set until it is genuinely satisfied.
//
// The package is pure — no daemon, no registry, no clock. Callers assemble an
// Observation from whatever they can see and pass `now` explicitly, so the
// whole lifecycle is hermetically testable.
//
// Three words, deliberately not interchangeable:
//
//	observation  what the world looks like right now (idle, working, reaped)
//	step         an action taken toward closing the gap (event to the PO,
//	             re-pressure deliver, overseer noise, human alert)
//	satisfaction the desired state actually holding
//
// Only an observation can satisfy a gap. A step never can — delivering
// worker-idle to a parent, or nudging the worker once, records progress
// toward satisfaction and nothing more. See docs/design/convergence-impatience.md.
package converge

import (
	"strings"
	"time"
)

// GapKey identifies a tracked gap. Today that is the agent name; the mission
// it was opened against is carried on the MissionGap, so a re-bound agent starts
// a new episode rather than silently inheriting the old one.
type GapKey string

// KeyFor normalizes an agent name into a GapKey.
func KeyFor(agent string) GapKey { return GapKey(strings.TrimSpace(agent)) }

// Condition is what a single Observation says about the desired state.
type Condition string

const (
	// ConditionGap: the mission is open and the agent is not working it.
	ConditionGap Condition = "gap"
	// ConditionSatisfied: the desired state holds — working on the open
	// mission, or the mission is closed/reaped with evidence.
	ConditionSatisfied Condition = "satisfied"
	// ConditionOutOfScope: nothing is desired of this agent right now
	// (not a work agent, deliberate stop, design-gated, no open mission).
	// Explicitly NOT satisfaction: scoping out is not achievement.
	ConditionOutOfScope Condition = "out_of_scope"
)

// GapKind labels why the mission is not being worked, so the actuator (🎯T315)
// and the noise ladder (🎯T317) can route differently without re-deriving it.
type GapKind string

const (
	// GapKindIdle: process running, phase idle/blocked, mission open.
	GapKindIdle GapKind = "idle_open_mission"
	// GapKindDeadHandle: mission open but the process is gone (🎯T313 corpse).
	GapKindDeadHandle GapKind = "dead_handle_open_mission"
	// GapKindUnverifiedDone: the agent claims done but no ledger closure and
	// no reap has happened. Attestation is not execution (🎯T31.1) — route to
	// the parent to verify and reap, do not re-pressure the worker.
	GapKindUnverifiedDone GapKind = "unverified_done"
)

// Observation is a pure snapshot of one agent. Callers fill it from the fleet
// registry, the progress hub, and the ledger; the package does no I/O.
type Observation struct {
	Name    string
	Purpose string // work | aside | overseer (empty = work)

	// Phase is the agent's live phase: working | idle | blocked | "" (unknown).
	Phase string
	// ProcessRunning is false for a registered agent whose handle/session died.
	ProcessRunning bool

	// TargetID is the mission the agent is bound to ("" = unbound implementer).
	TargetID string
	// MissionOpen is true while the desired state is still wanted: the bound
	// target is open, or an unbound implementer has not been accounted for.
	MissionOpen bool
	// MissionClosed is evidence-backed closure — the ledger says achieved /
	// retired / set aside. It is not a claim by the agent.
	MissionClosed bool
	// Reaped is true once the agent has been stopped and removed from the
	// live fleet (🎯T165 / 🎯T195).
	Reaped bool
	// ClaimsDone is a terminal report asserting completion. On its own this is
	// prose, not evidence, and does not satisfy anything.
	ClaimsDone bool

	// DeliberateStop / DesignGated scope the agent out rather than satisfy it.
	DeliberateStop bool
	DesignGated    bool
}

const purposeWork = "work"

// ClassifyObservation is the decidable core of the doctrine: it maps one
// observation to gap / satisfied / out-of-scope, and never consults step
// history — no number of steps can change this answer.
func ClassifyObservation(o Observation) (Condition, GapKind, string) {
	purpose := strings.TrimSpace(strings.ToLower(o.Purpose))
	if purpose == "" {
		purpose = purposeWork
	}
	if purpose != purposeWork {
		return ConditionOutOfScope, "", "not_work_purpose"
	}
	// Evidence-backed closure and reap are the only non-working satisfactions.
	if o.MissionClosed {
		return ConditionSatisfied, "", "mission_closed"
	}
	if o.Reaped {
		return ConditionSatisfied, "", "agent_reaped"
	}
	if o.DeliberateStop {
		return ConditionOutOfScope, "", "deliberate_stop"
	}
	if o.DesignGated {
		return ConditionOutOfScope, "", "design_gated"
	}
	if !o.MissionOpen {
		return ConditionOutOfScope, "", "no_open_mission"
	}
	if strings.EqualFold(strings.TrimSpace(o.Phase), "working") {
		return ConditionSatisfied, "", "working_on_open_mission"
	}
	switch {
	case o.ClaimsDone:
		return ConditionGap, GapKindUnverifiedDone, "claims_done_without_evidence"
	case !o.ProcessRunning:
		return ConditionGap, GapKindDeadHandle, "process_gone_mission_open"
	default:
		return ConditionGap, GapKindIdle, "not_working_mission_open"
	}
}

// StepKind names an action taken toward closing a gap. The list is open —
// 🎯T315 owns the re-pressure actuator, 🎯T317 owns the noise ladder — but
// every kind shares one property: recording it never satisfies the gap.
type StepKind string

const (
	// StepEventParent: worker-idle (or equivalent) delivered to the parent PO.
	StepEventParent StepKind = "event_parent"
	// StepRePressure: a real brief-or-continue deliver to the agent itself.
	StepRePressure StepKind = "re_pressure"
	// StepRehydrate: restart / re-attach of a dead handle.
	StepRehydrate StepKind = "rehydrate"
	// StepOverseerNoise: elevated event to the root overseer (🎯T317 rung).
	StepOverseerNoise StepKind = "overseer_noise"
	// StepHumanAlert: owner-visible sticky alert (🎯T317 final rung).
	StepHumanAlert StepKind = "human_alert"
)

// StepRecord is one attempt, successful or not.
type StepRecord struct {
	Key       GapKey
	Kind      StepKind
	At        time.Time
	Delivered bool
	Err       string
}

// MissionGap is a desired-state gap that is standing open in the reconcile set.
// It is returned by value; the set owns the originals.
type MissionGap struct {
	Key      GapKey
	Agent    string
	Mission  string  // TargetID at open time ("" = unbound implementer)
	Kind     GapKind // latest classification
	Reason   string  // latest classification reason
	OpenedAt time.Time
	// LastSeen is the time of the most recent observation that kept this gap open.
	LastSeen time.Time
	// Episode counts how many times this agent has entered a gap, including
	// this one. It survives satisfaction so the ladder (🎯T317) and the
	// attenuation policy (🎯T318) can see a flapping worker.
	Episode int

	// Steps taken during this episode, oldest first.
	Steps []StepRecord
	// StepsByKind counts delivered-or-attempted steps of each kind this episode.
	StepsByKind map[StepKind]int
	// LastStepAt is the time of the most recent step attempt (zero = none).
	LastStepAt time.Time
}

// Age is how long this gap has been open at now.
func (g MissionGap) Age(now time.Time) time.Duration {
	if g.OpenedAt.IsZero() {
		return 0
	}
	return now.Sub(g.OpenedAt)
}

// SinceLastStep is how long since the last step attempt. When no step has been
// taken it returns the gap's age, so a fresh gap is immediately actionable.
func (g MissionGap) SinceLastStep(now time.Time) time.Duration {
	if g.LastStepAt.IsZero() {
		return g.Age(now)
	}
	return now.Sub(g.LastStepAt)
}

// StepCount is the total number of step attempts this episode.
func (g MissionGap) StepCount() int { return len(g.Steps) }

// Satisfied is always false for a MissionGap. It exists so the type states the
// invariant out loud: a gap in the set is unsatisfied by construction, and
// nothing a step does can change that — only a fresh Observation can.
func (g MissionGap) Satisfied() bool { return false }

func (g MissionGap) clone() MissionGap {
	out := g
	if len(g.Steps) > 0 {
		out.Steps = append([]StepRecord(nil), g.Steps...)
	}
	if len(g.StepsByKind) > 0 {
		out.StepsByKind = make(map[StepKind]int, len(g.StepsByKind))
		for k, v := range g.StepsByKind {
			out.StepsByKind[k] = v
		}
	}
	return out
}

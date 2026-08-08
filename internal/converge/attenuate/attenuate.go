// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package attenuate is the pure progress-attenuation policy for 🎯T318.
//
// The convergence engine keeps a standing desired-state gap whenever an
// open-mission agent is not working (🎯T316 owns that reconcile set), and
// climbs an escalation ladder while the gap stays unsatisfied: re-pressure
// the agent (🎯T315), then noise to the root overseer, then owner-visible
// chrome (🎯T317).
//
// This package answers one question: given visible progress toward closing
// the gap, how much should the ladder slow down and quieten? It modulates
// 🎯T317's timing — it does not reimplement the ladder. The consumer seam is
// two values per agent per tick:
//
//	adj := att.Adjust(now, agent, dwell)
//	rung := dueRung(adj.EffectiveDwell, now, st)   // thresholds shift out
//	if rung > Rung(adj.Ceiling) { rung = Rung(adj.Ceiling) }  // noise capped
//
// Ceiling ordinals are chosen to match converge.Rung exactly, so the clamp is
// a cast and not a translation table. This package deliberately does not
// import converge: the policy is pure and must stay testable on its own.
//
// Three lines are load-bearing, and the tests exist to hold them:
//
//   - Progress is never satisfaction. Nothing here can clear a gap. Only
//     🎯T316's verdict — the agent working on an open mission, or the mission
//     closed/reaped — ends one. A restart, a spawn, a delivered re-pressure:
//     each buys quiet, none closes anything, and the gap stays in the set.
//   - Attenuation is bounded and temporary. Credit is capped per gap and over
//     the gap's whole life, and it lapses when progress stops. An unbroken
//     drip of progress signals still arrives at the human rung. There is no
//     freeze and no reset-to-zero-forever.
//   - De-escalation floors at the re-pressure rung. Progress can silence the
//     overseer and the owner; it can never silence the actuator, because the
//     gap is still open and the agent still needs pushing.
package attenuate

import (
	"fmt"
	"time"
)

// Ceiling is the loudest rung the ladder may reach while attenuation holds.
// The ordinals mirror converge.Rung (🎯T317) so a consumer clamps with a
// plain cast: if rung > converge.Rung(adj.Ceiling) { rung = ... }.
type Ceiling int

const (
	// CeilingNone silences the whole ladder. Never produced by this policy —
	// an open gap always keeps at least its actuator — and present only so
	// the ordinals line up with converge.RungNone.
	CeilingNone Ceiling = iota
	// CeilingRepressure permits the 🎯T315 actuator only. This is the floor:
	// progress buys quiet from the overseer and the owner, never from the
	// agent that is still sitting on an open mission.
	CeilingRepressure
	// CeilingOverseer permits noise up to the root overseer event.
	CeilingOverseer
	// CeilingHuman permits the full ladder including owner-visible chrome.
	CeilingHuman
)

func (c Ceiling) String() string {
	switch c {
	case CeilingNone:
		return "none"
	case CeilingRepressure:
		return "repressure"
	case CeilingOverseer:
		return "overseer-noise"
	case CeilingHuman:
		return "human-alert"
	}
	return fmt.Sprintf("ceiling(%d)", int(c))
}

// SignalKind classifies one thing the engine saw happen against an open gap.
// The progress / not-progress split is doctrine, not taste: telling somebody
// is a step, and a step is not progress until somebody actually moves.
type SignalKind int

const (
	// SignalUnknown is an unclassified observation. Never progress.
	SignalUnknown SignalKind = iota

	// SignalRepressureDelivered is the engine's own 🎯T315 deliver landing on
	// the agent. Weak progress: it buys delay but never lowers the ceiling,
	// because an engine that de-escalated on its own actions would be scoring
	// its noise as its own cure.
	SignalRepressureDelivered

	// SignalParentWorking is the root overseer or the PO entering working and
	// delivering re-pressure — a responsible agent genuinely picked this up.
	SignalParentWorking

	// SignalRestart is a restart or rehydrate of the parent or the stuck
	// implementer.
	SignalRestart

	// SignalSpawn is a research or fix worker spawned against this gap.
	SignalSpawn

	// SignalTargetWorking is the stuck agent transitioning idle→working.
	// Strong progress here — and separately what 🎯T316 reads as
	// satisfaction. This package never makes that second call.
	SignalTargetWorking

	// SignalNotifyOnly is an event fired at somebody with nothing behind it.
	// Not progress (🎯T318 acceptance 2).
	SignalNotifyOnly

	// SignalEmptyAck is an acknowledgement with no actuation behind it.
	// Not progress (🎯T318 acceptance 2).
	SignalEmptyAck
)

// IsProgress reports whether the signal counts as visible progress toward
// closing the gap, and so buys attenuation.
func (k SignalKind) IsProgress() bool {
	switch k {
	case SignalRepressureDelivered, SignalParentWorking, SignalRestart, SignalSpawn, SignalTargetWorking:
		return true
	}
	return false
}

// IsStrong reports whether the progress came from somebody other than the
// impatience engine, and so may also lower the noise ceiling. Weak progress
// still buys delay.
func (k SignalKind) IsStrong() bool {
	switch k {
	case SignalParentWorking, SignalRestart, SignalSpawn, SignalTargetWorking:
		return true
	}
	return false
}

func (k SignalKind) String() string {
	switch k {
	case SignalRepressureDelivered:
		return "repressure-delivered"
	case SignalParentWorking:
		return "parent-working"
	case SignalRestart:
		return "restart"
	case SignalSpawn:
		return "spawn"
	case SignalTargetWorking:
		return "target-working"
	case SignalNotifyOnly:
		return "notify-only"
	case SignalEmptyAck:
		return "empty-ack"
	}
	return "unknown"
}

// Signal is one observed event against one agent's open gap.
type Signal struct {
	Kind   SignalKind
	At     time.Time
	Detail string
}

// Policy holds the documented bounds. Defaults are order-of-minutes, sized
// against 🎯T317's ladder (re-pressure 4m, overseer 15m, human 45m) so that
// one real intervention buys roughly one ladder step of quiet and no more.
type Policy struct {
	// Credit is the delay one progress signal buys, before stall-strike
	// halving.
	Credit time.Duration
	// MinCredit floors the halving, so repeated stalls degrade a signal
	// toward negligible rather than to nothing.
	MinCredit time.Duration
	// MaxCredit caps outstanding delay for one gap. This is the never-freeze
	// bound: however many signals arrive, the ladder is never held back by
	// more than this at a time.
	MaxCredit time.Duration
	// MaxLifetimeCredit caps credit granted across the gap's whole life. This
	// is the never-reset-forever bound: total attenuation is finite, so a gap
	// that never reaches satisfaction always arrives at the human rung.
	MaxLifetimeCredit time.Duration
	// StallBound is how long attenuation survives without a further progress
	// signal. Past it the credit is void and the ceiling is restored, and
	// impatience climbs again (🎯T318 acceptance 3).
	StallBound time.Duration
}

// DefaultPolicy is the documented default attenuation for an open-mission
// idle gap.
func DefaultPolicy() Policy {
	return Policy{
		Credit:            8 * time.Minute,
		MinCredit:         1 * time.Minute,
		MaxCredit:         20 * time.Minute,
		MaxLifetimeCredit: 90 * time.Minute,
		StallBound:        12 * time.Minute,
	}
}

// State is one agent's attenuation state. Exported so the policy can be
// driven as a pure fold in tests and by a caller that owns its own storage.
type State struct {
	// Credit is outstanding delay subtracted from the ladder's dwell.
	Credit time.Duration
	// Granted is credit granted over the gap's life, against
	// MaxLifetimeCredit.
	Granted time.Duration
	// Ceiling is the loudest rung currently permitted.
	Ceiling Ceiling
	// LastProgressAt is the most recent progress signal, for stall detection.
	LastProgressAt time.Time
	// LastStallAt is the most recent lapse, so one quiet stretch counts as
	// one strike however many ticks observe it.
	LastStallAt time.Time
	// Strikes counts stall episodes. Each one halves what the next progress
	// signal is worth: progress that never converges buys less and less.
	Strikes int
	// Progress and Strong are bookkeeping for logs and tests.
	Progress int
	Strong   int
}

// Adjustment is the modulation for one agent on one tick.
type Adjustment struct {
	// EffectiveDwell is the gap's dwell minus outstanding credit, floored at
	// zero. Feed this to the ladder in place of the raw dwell and every rung
	// threshold shifts out by the credit.
	EffectiveDwell time.Duration
	// Ceiling is the loudest rung permitted this tick.
	Ceiling Ceiling
	// Credit is the outstanding delay behind EffectiveDwell.
	Credit time.Duration
	// Attenuated says progress is currently holding the ladder back.
	Attenuated bool
	// Stalled says attenuation lapsed on this tick for want of progress.
	Stalled bool
	// GapOpen is always true. Attenuation cannot close a gap (🎯T318
	// acceptance 4); it is here so a caller cannot read quiet as closure.
	GapOpen bool
	// Reason is a short trace for logs and test assertions.
	Reason string
}

// Backoff modulates 🎯T315's actuator: it extends the base wait before the
// next re-pressure by the delay progress has bought. This is the second
// consumer seam, and the whole of the T315 adoption:
//
//	need := NextNudgeBackoff(o.NudgeCount, o.Backoffs)
//	need = adj.Backoff(need)
//
// The added delay is capped at base, so attenuation can at most double the
// interval between re-pressures. Re-pressure slows while somebody is visibly
// working the gap; it never stops, because the gap is still open and the
// agent is still sitting on it. With no credit this is the identity, so an
// unattenuated caller is unchanged.
func (a Adjustment) Backoff(base time.Duration) time.Duration {
	if a.Credit <= 0 || base <= 0 {
		return base
	}
	return base + min(a.Credit, base)
}

// Observe folds one signal into the state and returns the next one. Pure: no
// clock, no I/O. Non-progress signals (notify-only, empty ack) are recorded
// as seen and change nothing.
func (p Policy) Observe(s State, sig Signal, now time.Time) State {
	s = p.seed(s)
	if !sig.Kind.IsProgress() {
		return s
	}

	at := sig.At
	if at.IsZero() || at.After(now) {
		at = now
	}
	if at.After(s.LastProgressAt) {
		s.LastProgressAt = at
	}
	s.Progress++

	// What this signal is worth: halved once per stall strike, floored at
	// MinCredit, then clipped by the per-gap and lifetime caps in turn.
	shift := min(s.Strikes, 16)
	grant := max(p.Credit>>uint(shift), p.MinCredit)
	if headroom := p.MaxCredit - s.Credit; grant > headroom {
		grant = headroom
	}
	if remaining := p.MaxLifetimeCredit - s.Granted; grant > remaining {
		grant = remaining
	}
	if grant > 0 {
		s.Credit += grant
		s.Granted += grant
	}

	// Strong progress also steps the noise ceiling down one rung. The floor
	// is CeilingRepressure: quiet from the overseer and the owner, never from
	// the actuator, because the gap is still open.
	if sig.Kind.IsStrong() {
		s.Strong++
		if s.Ceiling > CeilingRepressure {
			s.Ceiling--
		}
	}
	return s
}

// Adjust reports the modulation for this tick and folds in stall detection.
// dwell is the ladder's raw dwell for this gap (now minus gap open).
func (p Policy) Adjust(s State, dwell time.Duration, now time.Time) (State, Adjustment) {
	s = p.seed(s)

	// Attenuation is temporary. Without a further progress signal inside
	// StallBound the credit is void, the ceiling is restored, and the ladder
	// resumes its climb — with a strike that halves what future progress is
	// worth, so a drip of token progress cannot hold impatience off forever.
	stalled := false
	if !s.LastProgressAt.IsZero() && now.Sub(s.LastProgressAt) > p.StallBound {
		if s.Credit > 0 || s.Ceiling < CeilingHuman {
			stalled = true
		}
		s.Credit = 0
		s.Ceiling = CeilingHuman
		if s.LastProgressAt.After(s.LastStallAt) {
			s.Strikes++
			s.LastStallAt = now
			stalled = true
		}
	}

	effective := max(dwell-s.Credit, 0)

	adj := Adjustment{
		EffectiveDwell: effective,
		Ceiling:        s.Ceiling,
		Credit:         s.Credit,
		Attenuated:     s.Credit > 0 || s.Ceiling < CeilingHuman,
		Stalled:        stalled,
		GapOpen:        true,
	}
	switch {
	case stalled:
		adj.Reason = fmt.Sprintf("no progress within %s; attenuation lapsed, climbing again (strike %d)", p.StallBound, s.Strikes)
	case adj.Attenuated:
		adj.Reason = fmt.Sprintf("progress bought %s of delay, noise capped at %s; gap still open", s.Credit, s.Ceiling)
	default:
		adj.Reason = "no attenuation; ladder runs at full timing"
	}
	return s, adj
}

// seed gives a zero State its open-gap defaults: full ladder, no credit.
func (p Policy) seed(s State) State {
	if s.Ceiling == CeilingNone {
		s.Ceiling = CeilingHuman
	}
	return s
}

// Attenuator holds attenuation state for many agents across reconcile ticks,
// mirroring 🎯T317's Ladder. Not safe for concurrent use; the converge loop
// drives it from one goroutine.
type Attenuator struct {
	policy Policy
	agents map[string]State
}

// NewAttenuator returns an empty attenuator under the given policy. A zero
// Policy is replaced by DefaultPolicy.
func NewAttenuator(p Policy) *Attenuator {
	if p == (Policy{}) {
		p = DefaultPolicy()
	}
	return &Attenuator{policy: p, agents: map[string]State{}}
}

// Observe records one signal against an agent's gap.
func (a *Attenuator) Observe(agent string, sig Signal, now time.Time) {
	if agent == "" {
		return
	}
	a.agents[agent] = a.policy.Observe(a.agents[agent], sig, now)
}

// Adjust returns this tick's modulation for an agent's gap.
func (a *Attenuator) Adjust(agent string, dwell time.Duration, now time.Time) Adjustment {
	s, adj := a.policy.Adjust(a.agents[agent], dwell, now)
	if agent != "" {
		a.agents[agent] = s
	}
	return adj
}

// Forget drops an agent's attenuation state. Call it only when 🎯T316 says
// the gap reached satisfaction or left the reconcile set — never because the
// agent looked busy, which is exactly the progress-as-satisfaction error this
// package exists to prevent.
func (a *Attenuator) Forget(agent string) {
	delete(a.agents, agent)
}

// State exposes an agent's attenuation state for logs and tests.
func (a *Attenuator) State(agent string) State {
	return a.agents[agent]
}

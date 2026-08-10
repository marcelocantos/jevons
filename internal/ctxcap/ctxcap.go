// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ctxcap decides when an agent's conversation has grown large
// enough to compact (🎯T392.1).
//
// Why a ceiling at all: every model call resends the whole conversation,
// so a session's cost is quadratic in its length, and total fleet spend
// is linear in how long agents run before compacting. The 🎯T392 baseline
// measured 907.1M input tokens across 4,848 calls at a 187k mean context
// — 83% of it spent by coordinators, whose contexts grew 1.2-3.2k per
// turn and reached 399k before anything intervened.
//
// Why a mechanical threshold rather than the model's judgement: an agent
// deciding whether its own context is too large is exactly the judgement
// this replaces. It is also the judgement that failed — the only thing
// capping the overseer during the incident was accidental daemon
// restarts, roughly one every five hours.
//
// This package is pure. It reads no files, holds no state, and makes no
// decisions about *how* to compact — the daemon supplies observed context
// and performs the rotation.
package ctxcap

import (
	"fmt"
	"time"
)

// DefaultCeiling is the per-call context ceiling in tokens.
//
// 100k is chosen from the baseline replay, which charged each compaction
// a full turn at the ceiling plus a re-read penalty on the turns that
// follow: 200k gives -41%, 100k gives -63%, 50k gives -73% but triples
// the compaction count to 209. 100k keeps a coordinator able to hold a
// real mission in mind while cutting the dominant term.
const DefaultCeiling int64 = 100_000

// MinCeiling guards against a ceiling so low the fleet thrashes: below
// this an agent would compact every few turns and spend more on handovers
// than it saves.
const MinCeiling int64 = 20_000

// Verdict is what the governor should do about one agent.
type Verdict string

const (
	// VerdictOK — under the ceiling; nothing to do.
	VerdictOK Verdict = "ok"
	// VerdictCompact — over the ceiling; rotate onto a fresh session with
	// a handover pointer to the predecessor's transcript.
	VerdictCompact Verdict = "compact"
	// VerdictUnknown — no context observation available. Never compact on
	// an unknown: a missing measurement is not evidence of a small
	// context, and acting on it would rotate agents at random.
	VerdictUnknown Verdict = "unknown"
	// VerdictHold — over the ceiling, but compacted too recently to do it
	// again without thrashing. Distinct from OK so a persistent hold is
	// visible as "this agent lives above the ceiling" rather than passing
	// as healthy.
	VerdictHold Verdict = "hold"
)

// Decision is one agent's evaluation, carrying its own explanation so the
// eventlog and the owner see why a rotation happened.
type Decision struct {
	Agent   string  `json:"agent"`
	Verdict Verdict `json:"verdict"`
	Context int64   `json:"context"`
	Ceiling int64   `json:"ceiling"`
	Reason  string  `json:"reason"`
}

// Observation is what the daemon measured for one agent. Context is the
// tokens the agent's most recent model call carried, read from the
// provider's own usage frames — never estimated from characters.
type Observation struct {
	Agent   string
	Context int64
	// HasContext is false when no usage frame could be read (a cold agent
	// that has not taken a turn yet, or an unreadable session log).
	HasContext bool
	// Exempt marks agents the ceiling must not rotate. The owner's own
	// chat continuity is not a spend lever.
	Exempt bool
	// SinceLastCompaction is how long ago this agent was last rotated by
	// the ceiling. Zero means never, or unknown.
	SinceLastCompaction time.Duration
	// Now is the observation time, used only to interpret the above.
	Now time.Time
}

// DefaultMinInterval is the floor on how often one agent may be
// compacted (🎯T392.1 hysteresis).
//
// Compaction hands the successor its predecessor's transcript, which it
// reads — so an agent whose steady-state context exceeds the ceiling
// rotates, re-reads, re-exceeds, and rotates again. Observed 2026-08-10
// with no interval at all: the overseer rotated five times in 23 minutes
// (13:08, 13:12, 13:16, 13:23, 13:31) and then stayed down. Each cycle
// costs a full read and discards continuity, so the treadmill is worse
// than the unbounded context it was meant to fix.
//
// A ceiling only helps agents whose context grows THROUGH it. For one
// that lives above it, the honest answer is a higher ceiling or a
// cheaper handover, not more rotations.
const DefaultMinInterval = 30 * time.Minute

// Policy is the configured ceiling.
type Policy struct {
	// Ceiling in tokens; zero means DefaultCeiling. Below MinCeiling it is
	// raised to MinCeiling rather than honoured, because a ceiling that
	// causes constant rotation costs more than it saves.
	Ceiling int64
	// Disabled turns enforcement off entirely, leaving observation intact
	// so the spend report still shows what the ceiling would have done.
	Disabled bool
	// MinInterval is the floor between compactions of the SAME agent.
	// Zero means DefaultMinInterval; negative disables hysteresis, which
	// is what produced the observed treadmill and exists only for tests.
	MinInterval time.Duration
}

// EffectiveMinInterval resolves the configured hysteresis.
func (p Policy) EffectiveMinInterval() time.Duration {
	if p.MinInterval == 0 {
		return DefaultMinInterval
	}
	if p.MinInterval < 0 {
		return 0
	}
	return p.MinInterval
}

// EffectiveCeiling resolves the configured value against the defaults.
func (p Policy) EffectiveCeiling() int64 {
	if p.Ceiling <= 0 {
		return DefaultCeiling
	}
	if p.Ceiling < MinCeiling {
		return MinCeiling
	}
	return p.Ceiling
}

// Evaluate decides one agent's fate. Pure: same inputs, same verdict.
func (p Policy) Evaluate(obs Observation) Decision {
	ceiling := p.EffectiveCeiling()
	d := Decision{Agent: obs.Agent, Context: obs.Context, Ceiling: ceiling}
	switch {
	case p.Disabled:
		d.Verdict = VerdictOK
		d.Reason = "ceiling disabled"
	case !obs.HasContext:
		d.Verdict = VerdictUnknown
		d.Reason = "no usage frame observed — a missing measurement is not a small context"
	case obs.Exempt:
		d.Verdict = VerdictOK
		d.Reason = "exempt from the ceiling"
	case obs.Context > ceiling && obs.SinceLastCompaction > 0 &&
		obs.SinceLastCompaction < p.EffectiveMinInterval():
		// Over the ceiling, but rotated too recently. Compacting again
		// would be a treadmill: the successor re-reads its predecessor
		// and re-exceeds immediately. Hold, and say so loudly enough
		// that a ceiling which is simply too low for this agent shows up
		// as a repeated hold rather than as silent thrash.
		d.Verdict = VerdictHold
		d.Reason = fmt.Sprintf(
			"context %d exceeds ceiling %d but last compaction was %s ago (min %s) — holding rather than thrashing",
			obs.Context, ceiling, obs.SinceLastCompaction.Round(time.Second), p.EffectiveMinInterval())
	case obs.Context > ceiling:
		d.Verdict = VerdictCompact
		d.Reason = fmt.Sprintf("context %d exceeds ceiling %d", obs.Context, ceiling)
	default:
		d.Verdict = VerdictOK
		d.Reason = fmt.Sprintf("context %d within ceiling %d", obs.Context, ceiling)
	}
	return d
}

// EvaluateAll maps a fleet observation to decisions, preserving order so
// callers get a stable log.
func (p Policy) EvaluateAll(obs []Observation) []Decision {
	out := make([]Decision, 0, len(obs))
	for _, o := range obs {
		out = append(out, p.Evaluate(o))
	}
	return out
}

// Compactions counts the decisions that would rotate an agent.
func Compactions(ds []Decision) int {
	n := 0
	for _, d := range ds {
		if d.Verdict == VerdictCompact {
			n++
		}
	}
	return n
}

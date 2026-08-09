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

import "fmt"

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
}

// Policy is the configured ceiling.
type Policy struct {
	// Ceiling in tokens; zero means DefaultCeiling. Below MinCeiling it is
	// raised to MinCeiling rather than honoured, because a ceiling that
	// causes constant rotation costs more than it saves.
	Ceiling int64
	// Disabled turns enforcement off entirely, leaving observation intact
	// so the spend report still shows what the ceiling would have done.
	Disabled bool
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

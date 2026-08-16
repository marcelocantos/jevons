// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package turndepth decides when a single turn has run long enough that it
// should checkpoint and end rather than keep calling tools (🎯T392.4).
//
// Why depth is a spend lever at all: every model call in a turn resends the
// whole conversation *including every tool result so far*, so a turn's cost
// grows with the square of its own depth, on top of whatever the session
// already carries. The 🎯T392 baseline measured turns with 8+ model calls at
// 7.7% of turns but 20.5% of input — 186.2M of 907.1M tokens. The 4-7 band
// alone was 62.4% of turns, which is why the ceiling sits above it and not
// in it: ordinary work is a handful of calls, and a policy that fires there
// would tax the common case to bill the rare one.
//
// Why the enforcement is cooperative rather than pre-emptive, which is the
// whole design and the reason this package has two verdicts instead of one:
// by the time a turn reaches its Nth call, those N calls are ALREADY BILLED.
// Cutting the turn there saves only calls N+1..M while discarding whatever
// the turn had produced, which converts a depth problem into a cancellation
// problem — and cancelled turns are the sibling 🎯T392.5's whole subject,
// 6.6% of the frozen baseline billed for work thrown away. A hard interrupt
// at N would show as a win in the depth histogram and a loss in total spend.
//
// So at the ceiling the agent is ASKED to reach a checkpoint and end its
// turn, and it resumes from there in a fresh turn whose context 🎯T392.1 has
// had the chance to compact. The interrupt is the fallback for an agent that
// ignores the ask, it is off by default (see Policy.InterruptEnabled), and
// using it is meant to be visible rather than routine.
//
// This package is pure. It performs no I/O, holds no clock, and knows
// nothing about how the ask is delivered — the hook (cmd/turndepth) speaks
// to Claude-backed agents in band, and the daemon watches the same turns
// from the outside for the eventlog record and the fallback.
package turndepth

import "fmt"

// DefaultCeiling is the tool-call depth at which a turn is asked to
// checkpoint.
//
// 8 comes from the baseline replay: a cap of 8 lands at about -7% after
// charging each checkpoint a resumption turn, and it is the smallest of the
// 🎯T392 levers. It is deliberately just above the 4-7 band (62.4% of turns)
// so that the common shape of real work — read a few files, run a test,
// write an edit — never meets the policy at all.
const DefaultCeiling = 8

// MinCeiling is the floor on a configured ceiling. Below this the policy
// stops being a cap on runaway turns and starts being a tax on ordinary
// ones: at 4 it fires on the middle of the measured distribution, and every
// checkpoint costs a resumption turn that carries the same context again.
const MinCeiling = 4

// DefaultGrace is how many further tool calls a turn may make after being
// asked to checkpoint before the fallback is even considered.
//
// It is not slack for a disobedient agent; it is room for an obedient one.
// An agent told to checkpoint has to WRITE that checkpoint, and writing it
// is itself tool calls — save the notes, commit the work in progress. Firing
// the fallback the moment the ask goes unheeded would interrupt precisely
// the agents that were complying.
const DefaultGrace = 4

// Action is what the caller should do about one turn.
type Action string

const (
	// ActionNone — the turn is within its budget, or has already been asked
	// and is inside its grace. Nothing to do.
	ActionNone Action = "none"
	// ActionRequestCheckpoint — the turn has reached the ceiling: ask it to
	// checkpoint and end. Fires at most once per turn, because an ask
	// repeated on every subsequent call is noise the agent learns to skip.
	ActionRequestCheckpoint Action = "request_checkpoint"
	// ActionInterrupt — the ask was made, the grace is spent, and the turn is
	// still calling tools. Cut it. Off unless Policy.InterruptEnabled.
	ActionInterrupt Action = "interrupt"
)

// Policy is the configured depth ceiling.
type Policy struct {
	// Ceiling in tool calls per turn; zero means DefaultCeiling. Below
	// MinCeiling it is raised to MinCeiling rather than honoured.
	Ceiling int
	// Grace is the extra calls allowed after the ask before the fallback is
	// considered. Zero means DefaultGrace; negative means no grace at all,
	// which exists for tests and would be a poor production choice.
	Grace int
	// Disabled turns the policy off entirely. Counting continues in the
	// caller, so a disabled ceiling still shows in the depth histogram what
	// it would have done.
	Disabled bool
	// InterruptEnabled arms the fallback. OFF BY DEFAULT, deliberately.
	//
	// The in-band ask reaches Claude-backed agents through the PreToolUse
	// hook; an agent whose harness has no such hook — a Grok worker, a fresh
	// clone where `make turndepth` has not run — never hears the ask at all.
	// Arming the fallback by default would mean cutting those agents' deep
	// turns with no warning delivered first, which is exactly the
	// cancellation cost this design exists to avoid paying. The owner arms it
	// once the ask is known to be landing.
	InterruptEnabled bool
}

// EffectiveCeiling resolves the configured value against the defaults.
func (p Policy) EffectiveCeiling() int {
	if p.Ceiling <= 0 {
		return DefaultCeiling
	}
	if p.Ceiling < MinCeiling {
		return MinCeiling
	}
	return p.Ceiling
}

// EffectiveGrace resolves the configured grace.
func (p Policy) EffectiveGrace() int {
	if p.Grace == 0 {
		return DefaultGrace
	}
	if p.Grace < 0 {
		return 0
	}
	return p.Grace
}

// State is one turn as the caller has observed it so far.
type State struct {
	// Agent names the turn's owner, carried through so decisions log
	// themselves without the caller re-deriving who they were about.
	Agent string
	// Calls is the number of tool calls in this turn INCLUDING the one being
	// decided. A turn that has made none is at zero.
	Calls int
	// Requested records that this turn has already been asked to checkpoint.
	Requested bool
	// Interrupted records that the fallback has already fired for this turn,
	// so a turn that survives its own interrupt is not cut repeatedly.
	Interrupted bool
}

// Decision is one turn's evaluation, carrying its own explanation so the
// eventlog and the agent both see why depth mattered here.
type Decision struct {
	Agent   string `json:"agent"`
	Action  Action `json:"action"`
	Calls   int    `json:"calls"`
	Ceiling int    `json:"ceiling"`
	Reason  string `json:"reason"`
}

// Evaluate decides one turn's fate. Pure: same inputs, same action.
func (p Policy) Evaluate(st State) Decision {
	ceiling := p.EffectiveCeiling()
	d := Decision{Agent: st.Agent, Calls: st.Calls, Ceiling: ceiling}
	grace := p.EffectiveGrace()
	switch {
	case p.Disabled:
		d.Action = ActionNone
		d.Reason = "depth ceiling disabled"
	case st.Calls < ceiling:
		d.Action = ActionNone
		d.Reason = fmt.Sprintf("call %d of %d", st.Calls, ceiling)
	case !st.Requested:
		d.Action = ActionRequestCheckpoint
		d.Reason = fmt.Sprintf("call %d reached the depth ceiling of %d", st.Calls, ceiling)
	case st.Interrupted:
		// Already cut once. A turn that kept going after an interrupt is
		// telling the daemon the interrupt did not take, and cutting it again
		// on every subsequent call would be a loop, not an enforcement.
		d.Action = ActionNone
		d.Reason = fmt.Sprintf("call %d — already interrupted this turn", st.Calls)
	case !p.InterruptEnabled:
		d.Action = ActionNone
		d.Reason = fmt.Sprintf(
			"call %d is %d past the ceiling of %d; checkpoint was asked for and the interrupt fallback is disarmed",
			st.Calls, st.Calls-ceiling, ceiling)
	case st.Calls >= ceiling+grace:
		d.Action = ActionInterrupt
		d.Reason = fmt.Sprintf(
			"call %d is %d past the ceiling of %d and %d past the grace of %d — the checkpoint ask went unheeded",
			st.Calls, st.Calls-ceiling, ceiling, st.Calls-ceiling-grace, grace)
	default:
		d.Action = ActionNone
		d.Reason = fmt.Sprintf(
			"call %d is within the grace of %d after the checkpoint ask", st.Calls, grace)
	}
	return d
}

// CheckpointAsk is the text an agent reads when its turn hits the ceiling.
//
// It is generated here, next to the policy, rather than in the hook, so the
// words the agent acts on are covered by the same hermetic test as the
// decision that produces them. Two things it must say and one it must not:
// it must name the budget (so the agent can tell a policy from a bug) and
// what to do (checkpoint, then end the turn), and it must NOT imply the work
// is being thrown away — an agent that believes it has been cancelled starts
// over, which costs more than the depth did.
func CheckpointAsk(d Decision) string {
	return fmt.Sprintf(
		"🎯T392.4 depth ceiling: this turn has made %d tool calls, reaching the ceiling of %d.\n"+
			"\n"+
			"Nothing has been discarded and this is not an error. Every call in a turn resends "+
			"the whole conversation, so a long turn costs far more than the same work split "+
			"across two.\n"+
			"\n"+
			"Reach a checkpoint and END YOUR TURN:\n"+
			"  1. Write down where you are — commit work in progress, or state the next step "+
			"plainly in your reply so your successor turn can pick it up.\n"+
			"  2. End the turn. You will be resumed, and your context may be compacted first.\n"+
			"\n"+
			"This ask fires ONCE per turn. If the work genuinely cannot be split here, retry "+
			"the call and it will proceed.",
		d.Calls, d.Ceiling)
}

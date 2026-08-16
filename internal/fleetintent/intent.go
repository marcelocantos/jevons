// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package fleetintent is the shared answer to "should this agent be
// running?" — the question every fleet control was missing on 2026-08-10
// (🎯T414).
//
// Between roughly 06:00 and 06:30 that morning the overseer parked three
// workers under an Anthropic spend block and watched them resurrected three
// separate ways: the daemon restart reattached them, the sentinel issued a
// spawn mission against 34 ready leaves it read as neglect, and the
// impatience ladder fired repressure twice at each and closed the incidents
// as cleared. Every subsystem behaved correctly against its own model. None
// of them could represent that not running was the point, because each was
// keyed to the observed process — is something running? — and a stopped
// process looks identical whether it was parked deliberately, blocked by the
// provider, waiting on the owner, reaped for finishing, or died mid-turn.
//
// So intent is recorded here, deliberately and separately from observation,
// and every control that spawns, nudges, revives, repressures, or issues a
// repair mission consults it first. The load-bearing rule is [Allows]:
//
//	Observed process state alone never authorises a start.
//	Not-running is a fact, never a licence.
//
// The converse matters just as much and is the reason this package is not
// simply "a stop flag": an agent whose intent is Working and whose process
// died is still genuinely open, and 🎯T171 short-resume and 🎯T208 outage
// re-pressure must keep reviving it. A change that suppresses those is as
// wrong as the resurrections that prompted this package, and the oracle
// fails it in both directions.
package fleetintent

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// State is the deliberate answer to "should this agent be running?". It is
// set by an actor (owner, overseer, or a product path such as the 🎯T165 reap)
// and is never inferred from whether a process happens to be alive.
type State string

const (
	// Working: this agent should be running. A dead process under Working
	// intent is a fault to recover, which is what 🎯T171 and 🎯T208 do.
	Working State = "working"

	// Parked: the owner or overseer deliberately stood this agent down.
	// Nothing revives it until someone lifts the park (🎯T408: a deliberate
	// stop must survive a delivery, and a restart).
	Parked State = "parked"

	// BlockedProvider: the provider refuses work for a reason no retry can
	// fix — a spend limit, a revoked key, an account block (🎯T406). Nothing
	// spawns into a wall.
	BlockedProvider State = "blocked_provider"

	// BlockedOwner: the remaining work needs the owner, so no fleet action
	// can clear it — a class-3 keypress, a decision, an account. Pressuring
	// the agent spends turns on something it cannot do (🎯T413).
	BlockedOwner State = "blocked_owner"

	// Reaped: the agent finished and the product deregistered it (🎯T165 /
	// 🎯T195). It is named by no control: a notifier whose fleet view
	// outlives the registry must not instruct continue/re-brief/restart.
	Reaped State = "reaped"
)

// Unknown is the empty state: no deliberate intent has been recorded. It
// resolves to [Working] — see [Resolve] for why the default runs.
const Unknown State = ""

// AllStates lists every state in declaration order (oracle enumeration).
func AllStates() []State {
	return []State{Working, Parked, BlockedProvider, BlockedOwner, Reaped}
}

// Valid reports whether s is a state this package knows. Unknown is not
// valid as a stored value — it is the absence of one.
func Valid(s State) bool {
	switch s {
	case Working, Parked, BlockedProvider, BlockedOwner, Reaped:
		return true
	}
	return false
}

// Parse converts owner/overseer input to a State. It accepts the canonical
// spellings and the obvious aliases an operator types under pressure.
func Parse(s string) (State, error) {
	norm := strings.ToLower(strings.TrimSpace(s))
	norm = strings.ReplaceAll(norm, "-", "_")
	norm = strings.ReplaceAll(norm, " ", "_")
	switch norm {
	case "", "working", "work", "running", "active", "unpark", "unblock":
		return Working, nil
	case "parked", "park", "stood_down", "standdown", "stopped_deliberately":
		return Parked, nil
	case "blocked_provider", "provider_blocked", "provider", "spend_block",
		"spend_limit", "blocked_on_provider":
		return BlockedProvider, nil
	case "blocked_owner", "owner_blocked", "owner", "blocked_on_owner",
		"needs_owner":
		return BlockedOwner, nil
	case "reaped", "finished", "finished_and_reaped", "done", "deregistered":
		return Reaped, nil
	}
	return Unknown, fmt.Errorf("fleetintent: unknown state %q (want one of %s)",
		s, strings.Join(stateNames(), ", "))
}

func stateNames() []string {
	out := make([]string, 0, len(AllStates()))
	for _, s := range AllStates() {
		out = append(out, string(s))
	}
	return out
}

// Resolve maps a stored state to the one policy reads. The empty state
// resolves to Working, and that default is deliberate in both directions:
//
//   - It must not be a suppressing default. Every agent registered before
//     this package existed, and every agent spawned by a path that has not
//     yet learned to stamp intent, carries Unknown. A default of Parked
//     would silently stop the fleet reviving anything — the mirror-image
//     failure of the one this package exists to prevent.
//   - It is not a licence either. Unknown means nobody has said "do not run
//     this", not "a dead process may be started because it is dead". The
//     licence a control needs still comes from [Allows] reading an intent,
//     not from the process being absent.
func Resolve(s State) State {
	if s == Unknown {
		return Working
	}
	return s
}

// Runnable reports whether this state says the agent should be running.
// Only Working does.
func Runnable(s State) bool { return Resolve(s) == Working }

// Describe renders a state for an owner-facing line.
func Describe(s State) string {
	switch Resolve(s) {
	case Working:
		return "working"
	case Parked:
		return "parked by owner/overseer"
	case BlockedProvider:
		return "blocked on the provider"
	case BlockedOwner:
		return "blocked on the owner"
	case Reaped:
		return "finished and reaped"
	}
	return string(s)
}

// Control names a fleet control that can start, wake, or pressure an agent.
// Every control listed in 🎯T414 acceptance 2 has a constant here, and the
// oracle crosses all of them with every [State].
type Control string

const (
	// ControlSpawn: frontier-consume and PO proactive passes starting a new
	// worker for a ready leaf (🎯T155 / 🎯T193 / 🎯T254.1).
	ControlSpawn Control = "spawn"

	// ControlNudge: the worker-idle nudge sweep (🎯T207).
	ControlNudge Control = "nudge"

	// ControlRevive: dead-handle recovery and restart reattach, including
	// 🎯T171 short-resume (🎯T85).
	ControlRevive Control = "revive"

	// ControlRepressure: the impatience ladder raising a rung (🎯T317).
	ControlRepressure Control = "repressure"

	// ControlRepair: sentinel / staff-ops bounded repair missions (🎯T219).
	ControlRepair Control = "repair"

	// ControlDeliverStart: the send path starting a stopped agent so a
	// message has somewhere to land (🎯T408).
	ControlDeliverStart Control = "deliver_start"

	// ControlNotifyIdle: the worker-idle notification to a parent, which
	// instructs continue/re-brief/restart (🎯T413).
	ControlNotifyIdle Control = "notify_idle"
)

// AllControls lists every control in declaration order (oracle enumeration).
func AllControls() []Control {
	return []Control{
		ControlSpawn, ControlNudge, ControlRevive, ControlRepressure,
		ControlRepair, ControlDeliverStart, ControlNotifyIdle,
	}
}

// Decision is one control's answer for one agent, carrying the intent that
// produced it so the control can name the intent as its reason rather than
// reporting normal conditions.
type Decision struct {
	Allow   bool
	Control Control
	// Fleet and Agent are the resolved states this decision read.
	Fleet State
	Agent State
	// Blocking is whichever state declined (Unknown when Allow).
	Blocking State
	// Reason is a stable code: "intent_ok" when allowed, otherwise
	// "fleet_intent_<state>" or "agent_intent_<state>".
	Reason string
}

// String renders the decision for a log line or a report field.
func (d Decision) String() string {
	if d.Allow {
		return string(d.Control) + ": allowed (intent=working)"
	}
	return fmt.Sprintf("%s: declined — %s (%s)", d.Control, Describe(d.Blocking), d.Reason)
}

// ReasonCodeOK is the Reason of an allowing Decision.
const ReasonCodeOK = "intent_ok"

// FleetReason is the stable decline code for a fleet-wide intent.
func FleetReason(s State) string { return "fleet_intent_" + string(Resolve(s)) }

// AgentReason is the stable decline code for a per-agent intent.
func AgentReason(s State) string { return "agent_intent_" + string(Resolve(s)) }

// Allows is the whole policy: a control may act only when both the fleet and
// the agent are intended to be working. Fleet intent is checked first, since
// a provider wall is the more informative answer when both are set.
//
// There is deliberately no per-control escape hatch. Every control in
// [AllControls] either starts an agent, wakes one, or tells someone to — and
// none of those are the right response to "this is not supposed to be
// running". A control that later needs to act under a non-working intent
// should say so at its own call site, in the open, rather than by a quiet
// exemption here.
func Allows(fleet, agent State, c Control) Decision {
	f, a := Resolve(fleet), Resolve(agent)
	d := Decision{Control: c, Fleet: f, Agent: a}
	if f != Working {
		d.Blocking, d.Reason = f, FleetReason(f)
		return d
	}
	if a != Working {
		d.Blocking, d.Reason = a, AgentReason(a)
		return d
	}
	d.Allow, d.Reason = true, ReasonCodeOK
	return d
}

// AllowsFleet is [Allows] for a control with no existing agent to read —
// spawning a worker for a ready leaf, where the only intent in play is the
// fleet's.
func AllowsFleet(fleet State, c Control) Decision {
	return Allows(fleet, Working, c)
}

// Record is one stored intent with its provenance. Intent is a deliberate
// act, so who set it and why are part of the value: an owner reading the
// cockpit needs to know a fleet is parked *by them* under a spend block, not
// merely that it is parked.
type Record struct {
	State  State     `json:"state"`
	By     string    `json:"by,omitempty"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// Describe renders a record for an owner-facing line.
func (r Record) Describe() string {
	b := &strings.Builder{}
	b.WriteString(Describe(r.State))
	if r.By != "" {
		fmt.Fprintf(b, " by %s", r.By)
	}
	if r.Reason != "" {
		fmt.Fprintf(b, " (%s)", r.Reason)
	}
	if !r.At.IsZero() {
		fmt.Fprintf(b, " since %s", r.At.UTC().Format(time.RFC3339))
	}
	return b.String()
}

// Snapshot is a whole-fleet read: the fleet intent plus every agent that
// carries a deliberate one. Agents absent from Agents are Working.
type Snapshot struct {
	Fleet  Record            `json:"fleet"`
	Agents map[string]Record `json:"agents"`
}

// AgentState returns the resolved state for name.
func (s Snapshot) AgentState(name string) State {
	if s.Agents == nil {
		return Working
	}
	return Resolve(s.Agents[name].State)
}

// FleetState returns the resolved fleet-wide state.
func (s Snapshot) FleetState() State { return Resolve(s.Fleet.State) }

// Allow is [Allows] against this snapshot for one agent and control.
func (s Snapshot) Allow(name string, c Control) Decision {
	return Allows(s.FleetState(), s.AgentState(name), c)
}

// AllowSpawn is [AllowsFleet] against this snapshot: a new worker has no
// agent row yet, so only the fleet intent can decline it.
func (s Snapshot) AllowSpawn() Decision {
	return AllowsFleet(s.FleetState(), ControlSpawn)
}

// NotWorking lists the agents carrying a non-working intent, sorted by name,
// for an owner-facing summary.
func (s Snapshot) NotWorking() []string {
	var out []string
	for name, rec := range s.Agents {
		if Resolve(rec.State) != Working {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Summarize is a one-line fleet-intent summary for the cockpit or a log.
func (s Snapshot) Summarize() string {
	fleet := s.FleetState()
	held := s.NotWorking()
	if fleet == Working && len(held) == 0 {
		return "fleet intent: working (no agent stood down)"
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "fleet intent: %s", s.Fleet.Describe())
	if len(held) == 0 {
		return b.String()
	}
	fmt.Fprintf(b, "; %d stood down: ", len(held))
	parts := make([]string, 0, len(held))
	for _, name := range held {
		parts = append(parts, name+"="+string(s.AgentState(name)))
	}
	b.WriteString(strings.Join(parts, ", "))
	return b.String()
}

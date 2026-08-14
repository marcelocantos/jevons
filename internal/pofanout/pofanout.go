// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package pofanout decides whether an idle product owner is sleeping
// legitimately or has silently stopped fanning out (🎯T380).
//
// The doctrine for PO fan-out already exists and is thorough — 🎯T155
// continuous kick-off, 🎯T193 file→spawn same turn, 🎯T325.1
// proactive-until-empty, 🎯T111.4 multi-slice fan-out — and every word of it
// is instructional residual with nothing asserting it happened. A PO that
// answers an explicit fan-out order with silence therefore looks exactly like
// a PO correctly asleep on an all-gated frontier: both are "idle" in
// agent_list, and the difference is only visible to whoever reads the ledger
// beside the fleet. That reading is what this package performs.
//
// The distinction comes from the ledger, never from the agent's name: the
// caller passes the same [poproactive.LeafObs] set the frontier-consume sweep
// builds, and legitimate sleep is exactly [poproactive.Sleep] over it. Name
// shape decides only which registry rows are product owners at all, which is
// the caller's business (see isPOName in the mcpserver harness).
package pofanout

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/poproactive"
)

// Verdict is the fan-out reading of one product owner against its
// product-scoped frontier.
type Verdict string

const (
	// VerdictAbsent: the PO is not alive. Dead/stopped agents are the
	// dead_agent class (T85 / cockpit fleet health), not a fan-out fault.
	VerdictAbsent Verdict = "absent"
	// VerdictWorking: the PO is mid-turn. Nothing is decidable yet — a turn in
	// flight may still spawn.
	VerdictWorking Verdict = "working"
	// VerdictAnswered: a turn ended and new children appeared. This is the
	// distinguishing observation acceptance (3) asks for: a PO that answered a
	// spawn order is not the PO that dropped it.
	VerdictAnswered Verdict = "answered"
	// VerdictSleepOK: legitimate 🎯T325.1 sleep — the frontier is empty, or
	// only gated / blocked / parked / already-engaged leaves remain.
	VerdictSleepOK Verdict = "sleep_ok"
	// VerdictWithinGrace: idle on ready leaves, but not yet past the grace
	// bound. Between two turns a PO is briefly idle; that is not a fault.
	VerdictWithinGrace Verdict = "within_grace"
	// VerdictStalled: FAULT — idle past grace while the product-scoped
	// frontier holds ready, unengaged, non-gated leaves.
	VerdictStalled Verdict = "stalled"
	// VerdictTurnNoFanout: FAULT — a turn ran to completion and the PO is idle
	// again with zero new children while ready leaves waited. Strictly stronger
	// evidence than VerdictStalled: the PO was awake and spawned nothing. A
	// direct spawn order is delivered as exactly such a turn.
	VerdictTurnNoFanout Verdict = "turn_no_fanout"
)

// IsFault reports whether a verdict must be surfaced as a fault rather than
// presented as ordinary idle.
func IsFault(v Verdict) bool {
	return v == VerdictStalled || v == VerdictTurnNoFanout
}

// POObs is one product-owner observation, projected by the caller from the
// registry, the activity tracker, and its own cross-cycle bookkeeping.
type POObs struct {
	// Name is the registry name (reporting only — never classification input).
	Name string
	// Alive is true when the PO process is up.
	Alive bool
	// Phase is the activity-tracker phase: idle | working | busy | "" (unknown,
	// treated as idle, matching the idle-nudge sweep).
	Phase string
	// GraceElapsed is true once the PO has been idle-on-ready-leaves for longer
	// than the caller's grace bound.
	GraceElapsed bool
	// LiveWorkChildren is how many live work children this PO currently
	// parents (reporting; the fault does not depend on it — a PO with children
	// on other targets is still stalled on an unengaged ready leaf).
	LiveWorkChildren int
	// TurnEnded is true when a turn began and completed since the previous
	// observation — the PO was awake, and is idle again. Deliveries (owner
	// directs, overseer fan-out orders) arrive as turns, so this is how an
	// order that ended in silence is observed without instrumenting the send
	// path.
	TurnEnded bool
	// NewChildrenThisTurn is how many work children appeared across that turn.
	NewChildrenThisTurn int
}

// Result is the reading of one PO.
type Result struct {
	Name string
	// Verdict is the classification; IsFault says whether to surface it.
	Verdict Verdict
	// Reason is a stable code for the wire and for tests.
	Reason string
	// ReadyIDs are the ready, unengaged, non-gated leaves the PO is leaving
	// stranded (empty unless the frontier has any).
	ReadyIDs []string
	// Detail is the compact human line for the signal / eventlog.
	Detail string
}

// Fault reports whether this result is a fault.
func (r Result) Fault() bool { return IsFault(r.Verdict) }

// idlePhase reports whether a phase string means "not mid-turn". Unknown ("")
// counts as idle, matching ClassifyIdleNudge's reading of a tracker that has
// seen no events for an agent.
func idlePhase(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "", "idle":
		return true
	default:
		return false
	}
}

// Classify reads one PO against its product-scoped frontier leaves.
//
// Order matters. A PO that answered is reported as answered even if it is now
// legitimately asleep; legitimate sleep outranks both fault verdicts, so an
// all-gated frontier can never make silence a fault — a fan-out order over
// nothing spawnable is correctly answered with nothing.
func Classify(po POObs, leaves []poproactive.LeafObs) Result {
	name := strings.TrimSpace(po.Name)
	decision := poproactive.Classify(leaves)
	res := Result{Name: name, ReadyIDs: decision.ReadyIDs}

	switch {
	case !po.Alive:
		res.Verdict, res.Reason = VerdictAbsent, "po_not_alive"
	case !idlePhase(po.Phase):
		res.Verdict, res.Reason = VerdictWorking, "turn_in_flight"
	case po.TurnEnded && po.NewChildrenThisTurn > 0:
		res.Verdict, res.Reason = VerdictAnswered, "turn_spawned_children"
	case decision.Mode == poproactive.Sleep:
		// 🎯T325.1 sleep, straight from the ledger: empty_frontier or
		// only_gated_or_engaged.
		res.Verdict, res.Reason = VerdictSleepOK, decision.Reason
	case !po.GraceElapsed:
		res.Verdict, res.Reason = VerdictWithinGrace, "ready_leaves_within_grace"
	case po.TurnEnded:
		res.Verdict, res.Reason = VerdictTurnNoFanout, "turn_ended_zero_new_children"
	default:
		res.Verdict, res.Reason = VerdictStalled, "idle_on_ready_leaves"
	}

	res.Detail = FormatDetail(po, res)
	return res
}

// ClassifyAll reads every PO against the same product-scoped frontier.
func ClassifyAll(pos []POObs, leaves []poproactive.LeafObs) []Result {
	out := make([]Result, 0, len(pos))
	for _, po := range pos {
		if strings.TrimSpace(po.Name) == "" {
			continue
		}
		out = append(out, Classify(po, leaves))
	}
	return out
}

// Faults filters results down to the ones worth surfacing.
func Faults(results []Result) []Result {
	var out []Result
	for _, r := range results {
		if r.Fault() {
			out = append(out, r)
		}
	}
	return out
}

// maxDetailIDs bounds how many ready leaf ids a detail line cites, matching
// the frontier-stall detail cap so wire text stays compact.
const maxDetailIDs = 8

// FormatDetail builds the compact evidence line: what the PO was doing, and
// which ready leaves it left unengaged.
func FormatDetail(po POObs, res Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "po=%s verdict=%s reason=%s phase=%s children=%d",
		res.Name, res.Verdict, res.Reason, phaseOrUnknown(po.Phase), po.LiveWorkChildren)
	if po.TurnEnded {
		fmt.Fprintf(&b, " turn_ended=true new_children=%d", po.NewChildrenThisTurn)
	}
	if n := len(res.ReadyIDs); n > 0 {
		ids := res.ReadyIDs
		suffix := ""
		if n > maxDetailIDs {
			ids = ids[:maxDetailIDs]
			suffix = fmt.Sprintf(" +%d more", n-maxDetailIDs)
		}
		fmt.Fprintf(&b, " ready=%d [%s%s]", n, strings.Join(ids, ","), suffix)
	}
	return b.String()
}

func phaseOrUnknown(phase string) string {
	if p := strings.TrimSpace(phase); p != "" {
		return p
	}
	return "unknown"
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package poidlenudge classifies a product owner that is idle while its
// repo carries open, unblocked work with nobody engaged on it, or whose
// child workers are stopped holding deliverable messages that have sat
// queued past a threshold (🎯T1266).
//
// Specimens: 2026-10-28, pigeon-po sat idle ~5h with pigeon T66.2-T66.6
// open, every T66 worker stopped (three with messages queued ~5h) and a
// clean-gated fix unlanded; claudia-po left claudia T234 (owner GO already
// given) with its only worker stopped. Both blocked owner-visible jevons
// work until the owner session happened to notice.
//
// This package is pure: it takes an observation plus small cross-cycle
// state and returns a verdict. It never sends anything and never reads
// the clock itself — the caller (sentinel) supplies now and the grace
// windows, and persists State across cycles.
package poidlenudge

import "time"

// DefaultQueueThreshold is how long a stopped worker's oldest queued
// message may sit before its PO's silence counts as a symptom.
const DefaultQueueThreshold = 30 * time.Minute

// DefaultFaultGrace is how long after a nudge the symptom must persist
// before the sentinel escalates to one owner-visible fault.
const DefaultFaultGrace = 30 * time.Minute

// TargetObs is one bullseye target in this PO's repo, as seen by the
// sentinel before classification.
type TargetObs struct {
	ID string
	// Blocked is true when the target has an open (unachieved) dependency.
	Blocked bool
	// Parked is true for design-gated / set_aside / deferred / owner-assigned
	// targets — anything the frontier already excludes from ready work.
	Parked bool
	// OwnerQuestion is true when an open owner question blocks this target.
	OwnerQuestion bool
	// Engaged is true when a live worker is bound to this target.
	Engaged bool
}

// Stalled reports whether this target is open, unblocked, unparked, not
// waiting on the owner, and has nobody engaged on it.
func (t TargetObs) Stalled() bool {
	return !t.Blocked && !t.Parked && !t.OwnerQuestion && !t.Engaged
}

// StoppedWorkerObs is one of this PO's work children that is stopped while
// holding a message the PO never received.
type StoppedWorkerObs struct {
	Agent  string
	Target string
	// QueuedAge is how long the oldest queued deliverable message has sat
	// unread. Zero means no message is queued.
	QueuedAge time.Duration
}

// POObs is one product owner's state for this cycle.
type POObs struct {
	Name    string
	Alive   bool
	Idle    bool
	Targets []TargetObs
	// StoppedWorkers lists this PO's stopped work children carrying a
	// queued message, regardless of age; threshold filtering happens in
	// Classify so the caller does not have to pre-filter.
	StoppedWorkers []StoppedWorkerObs
	// CapacityCritical: fleet capacity is under critical pressure this
	// cycle. The symptom is deferred (state untouched), not dropped.
	CapacityCritical bool
}

// Verdict is what the sentinel should do this cycle.
type Verdict string

const (
	VerdictOK       Verdict = "ok"       // nothing to do
	VerdictDeferred Verdict = "deferred" // symptom present but capacity-gated
	VerdictNudge    Verdict = "nudge"    // first-stage: typed nudge to the PO
	VerdictFault    Verdict = "fault"    // second-stage: one owner-visible fault
)

// State is the cross-cycle bookkeeping Classify needs, owned and persisted
// by the caller (one per PO name).
type State struct {
	// NudgedAt is when the PO was last nudged for this symptom. Zero means
	// never nudged (or the symptom cleared since).
	NudgedAt time.Time
	// FaultedAt is when the owner-visible fault last fired. Zero means
	// never fired (or the symptom cleared since).
	FaultedAt time.Time
}

// Result is what Classify decided plus the evidence to put in the nudge or
// fault text.
type Result struct {
	Verdict        Verdict
	StalledTargets []string
	StoppedAgents  []string
	Detail         string
}

// Classify decides this cycle's action for one PO. faultGrace is how long
// after NudgedAt the symptom must persist before escalating; the caller
// should reset State to its zero value once the symptom has cleared, which
// un-arms both stages for the next occurrence.
func Classify(obs POObs, st State, now time.Time, queueThreshold, faultGrace time.Duration) (Result, State) {
	if queueThreshold <= 0 {
		queueThreshold = DefaultQueueThreshold
	}
	if faultGrace <= 0 {
		faultGrace = DefaultFaultGrace
	}

	if !obs.Alive || !obs.Idle {
		// Busy or dead POs are not silent on this frontier — nothing to
		// read yet. Leave State as-is: a turn in flight is not evidence the
		// symptom resolved.
		return Result{Verdict: VerdictOK}, st
	}

	var stalled, stoppedAgents []string
	for _, t := range obs.Targets {
		if t.Stalled() {
			stalled = append(stalled, t.ID)
		}
	}
	for _, w := range obs.StoppedWorkers {
		if w.QueuedAge >= queueThreshold {
			stoppedAgents = append(stoppedAgents, w.Agent)
		}
	}

	if len(stalled) == 0 && len(stoppedAgents) == 0 {
		// Symptom cleared (or never present): un-arm both stages.
		return Result{Verdict: VerdictOK}, State{}
	}

	if obs.CapacityCritical {
		// Deferred, not dropped: state is untouched so the clock the PO is
		// judged against does not restart just because capacity was tight
		// for one cycle.
		return Result{
			Verdict:        VerdictDeferred,
			StalledTargets: stalled,
			StoppedAgents:  stoppedAgents,
			Detail:         "deferred: fleet capacity critical",
		}, st
	}

	if st.NudgedAt.IsZero() {
		st.NudgedAt = now
		return Result{
			Verdict:        VerdictNudge,
			StalledTargets: stalled,
			StoppedAgents:  stoppedAgents,
			Detail:         "first nudge",
		}, st
	}

	if st.FaultedAt.IsZero() && now.Sub(st.NudgedAt) >= faultGrace {
		st.FaultedAt = now
		return Result{
			Verdict:        VerdictFault,
			StalledTargets: stalled,
			StoppedAgents:  stoppedAgents,
			Detail:         "unresolved past fault grace since nudge",
		}, st
	}

	return Result{
		Verdict:        VerdictOK,
		StalledTargets: stalled,
		StoppedAgents:  stoppedAgents,
		Detail:         "symptom persists, waiting out grace",
	}, st
}

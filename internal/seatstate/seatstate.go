// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatstate answers one question — what is true about this seat —
// so that nothing else has to guess (🎯T766.2).
//
// On 2026-09-21 a census found eleven independent derivations of seat state
// in this daemon and no aggregator (docs/fleet-census.md). They disagreed by
// construction rather than by race: a transcript fold read `working` while
// the load reaper read `!PromptInFlight` as idle and terminated the process
// group; the ACP tracker was empty whenever the event sink was dark, which
// is the exact fault another loop exists to repair. Twelve actuators then
// acted on those answers, each with its own notion of what it had learned.
//
// Two properties matter as much as unification, and both are structural
// here rather than conventional.
//
// Unknown is a value. Most of the recurring defects in this subsystem are an
// absence read as a state: no transcript means dead, no event means idle, a
// rate-limited meter means an exhausted plan. [Tri] has three values and
// there is no way to spell "probably". A caller that wants to act on truth
// must ask for truth and handle not knowing.
//
// Freshness is part of the answer. An observation is a fact about a moment,
// not a standing property, so a state older than [Args.Stale] degrades to
// unknown rather than being served as current. A supervisor that cannot see
// is not entitled to report calm.
//
// The fold and Get do no I/O. Separate feed adapters translate claudia seat
// reports, events and durable evidence into observations; controls never
// open a transcript or probe a process to answer a state query.
package seatstate

import (
	"sort"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/turnev"
)

// Tri is a boolean that admits not knowing. The zero value is Unknown, so a
// struct that was never filled in reports ignorance rather than falsehood —
// which is the bug this package exists to end.
type Tri int

const (
	// Unknown: nobody has told us, or what we were told has gone stale.
	Unknown Tri = iota
	// No is observed false, by the party that knows — not merely unseen.
	No
	// Yes is observed true.
	Yes
)

func (t Tri) String() string {
	switch t {
	case Yes:
		return "yes"
	case No:
		return "no"
	default:
		return "unknown"
	}
}

// Known reports whether t carries information.
func (t Tri) Known() bool { return t == Yes || t == No }

// TriOf lifts an ordinary bool from a source that definitely observed it.
// Callers that merely failed to see something must not use this.
func TriOf(b bool) Tri {
	if b {
		return Yes
	}
	return No
}

// QueueUnknown is the depth of a queue nobody has reported.
const QueueUnknown = -1

// Each independently observed signal has its own clock and provenance.
type signal uint8

const (
	providerSignal signal = iota
	modelSignal
	aliveSignal
	flightSignal
	birthSignal
	phaseSignal
	activitySignal
	queueSignal
	statusSignal
	localAliveSignal
	brokerAliveSignal
	brokerOwnedSignal
	ownerQueueSignal
	resumeSignal
	signalCount
)

// State is what is true about one seat, as far as anyone has told us.
//
// Every field can be unknown, and a stale State returns unknown for the
// fields that decay (see [Authority.Get]). Provider and Model do not decay:
// they are identity, not condition.
type State struct {
	Name      string
	SessionID string
	Provider  string
	Model     string

	// Alive is whether the seat's process exists, per claudia.
	Alive Tri
	// Local handle and broker reports remain distinguishable for reattachment.
	LocalAlive  Tri
	BrokerAlive Tri
	BrokerOwned Tri
	// ResumeLost is a located-absent conversation required by a resume.
	ResumeLost Tri
	// InFlight is whether a turn is running, per the provider — not per our
	// own memory of having sent something.
	InFlight Tri
	// Phase is the transcript's reading: idle, working, or unknown. It
	// shares turnev's vocabulary deliberately; a second vocabulary for the
	// same idea is how the eleven derivations happened.
	Phase turnev.Phase
	// Status is the observed lifecycle label; empty means unknown.
	Status string
	// LastActivity is when this seat last did anything observable. Zero
	// means nobody has said.
	LastActivity time.Time
	// QueueDepth is how many messages are waiting for this seat, or
	// QueueUnknown.
	QueueDepth int
	// OwnerQueueDepth is the separate owner notification queue.
	OwnerQueueDepth int
	// BornStuck is whether an accepted opening prompt never produced a
	// transcript past grace (🎯T679.2 / census derivation 7). Unknown is
	// the only honest answer when nobody has diagnosed this name.
	BornStuck Tri

	// Observed is when this state was learned, and Source names who said so.
	// They are part of the answer: a control deciding to kill a process is
	// entitled to know how old its evidence is and where it came from.
	Observed time.Time
	Source   string

	// Each signal ages independently; queue traffic cannot refresh liveness.
	fieldAt     [signalCount]time.Time
	fieldSource [signalCount]string
	sessionAt   time.Time
}

// Fresh reports whether s was observed within stale of now.
func (s State) Fresh(now time.Time, stale time.Duration) bool {
	if s.Observed.IsZero() || stale <= 0 {
		return false
	}
	return now.Sub(s.Observed) <= stale
}

// Observation is one report about a seat from one source. Only the fields a
// source actually knows should be set; the zero value of every field means
// "I am not telling you about this", so a partial report never overwrites a
// better-informed one with ignorance.
type Observation struct {
	Name      string
	SessionID string
	// ForSession guards a delayed feed without establishing a new incarnation.
	ForSession string
	Provider   string
	Model      string

	Alive       Tri
	LocalAlive  Tri
	BrokerAlive Tri
	BrokerOwned Tri
	ResumeLost  Tri
	InFlight    Tri
	Phase       turnev.Phase
	BornStuck   Tri
	Status      string

	// LastActivity zero means no claim.
	LastActivity time.Time
	// QueueDepth QueueUnknown means no claim.
	QueueDepth int
	// OwnerQueueDepth nil makes no claim about the owner delivery queue.
	OwnerQueueDepth *int

	// Source names the reporter: "claudia.info", "acp.event", "sendq".
	// Unnamed observations are refused, because an answer whose provenance
	// nobody recorded is how a wrong reading survives a post-mortem.
	Source string
	// At is when the observation was made. Zero means now.
	At time.Time
}

// Args configures an Authority.
type Args struct {
	// Now overrides the clock (tests).
	Now func() time.Time
	// Stale is how long an observation stands before its decaying fields
	// read Unknown. Zero uses DefaultStale.
	Stale time.Duration
}

// DefaultStale is chosen against the fleet's own cadence: the event sink
// reports continuously while a seat is working, and the slowest useful
// reporter runs every 30 seconds. Two minutes is long enough that an idle,
// healthy seat is not repeatedly declared unknown, and short enough that a
// severed feed becomes visible within one supervision pass rather than
// being mistaken for calm.
const DefaultStale = 2 * time.Minute

// Authority holds the current state of every seat anyone has reported.
type Authority struct {
	mu    sync.RWMutex
	seats map[string]State
	now   func() time.Time
	stale time.Duration
}

// New builds an Authority.
func New(args Args) *Authority {
	now := args.Now
	if now == nil {
		now = time.Now
	}
	stale := args.Stale
	if stale <= 0 {
		stale = DefaultStale
	}
	return &Authority{seats: map[string]State{}, now: now, stale: stale}
}

// Observe folds one report into the current state.
//
// A field the reporter did not claim leaves the previous value alone. That
// is the difference between a fold and a snapshot: the ACP stream knows the
// phase and nothing about the queue, the queue knows its depth and nothing
// about the provider, and neither should erase the other.
//
// An observation with no Name or no Source is ignored.
func (a *Authority) Observe(obs Observation) {
	if obs.Name == "" || obs.Source == "" {
		return
	}
	at := obs.At
	if at.IsZero() {
		at = a.now()
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	cur, seen := a.seats[obs.Name]
	if obs.ForSession != "" && cur.SessionID != obs.ForSession {
		return
	}
	if !seen {
		// QueueDepth is an int, so unlike Tri its zero value is a claim —
		// "no messages waiting" — which nobody has made. Establish ignorance
		// explicitly on first sight.
		cur.QueueDepth = QueueUnknown
		cur.OwnerQueueDepth = QueueUnknown
	}
	if obs.SessionID != "" && obs.SessionID != cur.SessionID {
		// A remint is a different seat incarnation. No condition from its
		// predecessor is evidence about the new session.
		if at.Before(cur.sessionAt) {
			return
		}
		ownerDepth := cur.OwnerQueueDepth
		cur = State{Name: obs.Name, SessionID: obs.SessionID, QueueDepth: QueueUnknown, OwnerQueueDepth: ownerDepth, sessionAt: at}
	}
	cur.Name = obs.Name

	if obs.ResumeLost.Known() && !at.Before(cur.fieldAt[resumeSignal]) {
		cur.ResumeLost = obs.ResumeLost
		cur.fieldAt[resumeSignal] = at
		cur.fieldSource[resumeSignal] = obs.Source
	}
	if obs.LocalAlive.Known() && !at.Before(cur.fieldAt[localAliveSignal]) {
		cur.LocalAlive = obs.LocalAlive
		cur.fieldAt[localAliveSignal] = at
		cur.fieldSource[localAliveSignal] = obs.Source
	}
	if obs.BrokerAlive.Known() && !at.Before(cur.fieldAt[brokerAliveSignal]) {
		cur.BrokerAlive = obs.BrokerAlive
		cur.fieldAt[brokerAliveSignal] = at
		cur.fieldSource[brokerAliveSignal] = obs.Source
	}
	if obs.BrokerOwned.Known() && !at.Before(cur.fieldAt[brokerOwnedSignal]) {
		cur.BrokerOwned = obs.BrokerOwned
		cur.fieldAt[brokerOwnedSignal] = at
		cur.fieldSource[brokerOwnedSignal] = obs.Source
	}
	if obs.Status != "" && !at.Before(cur.fieldAt[statusSignal]) {
		cur.Status = obs.Status
		cur.fieldAt[statusSignal] = at
		cur.fieldSource[statusSignal] = obs.Source
	}
	if obs.Provider != "" && !at.Before(cur.fieldAt[providerSignal]) {
		cur.fieldAt[providerSignal] = at
		cur.fieldSource[providerSignal] = obs.Source
		cur.Provider = obs.Provider
	}
	if obs.Model != "" && !at.Before(cur.fieldAt[modelSignal]) {
		cur.fieldAt[modelSignal] = at
		cur.fieldSource[modelSignal] = obs.Source
		cur.Model = obs.Model
	}
	if obs.Alive.Known() && !at.Before(cur.fieldAt[aliveSignal]) {
		cur.fieldAt[aliveSignal] = at
		cur.fieldSource[aliveSignal] = obs.Source
		cur.Alive = obs.Alive
	}
	if obs.InFlight.Known() && !at.Before(cur.fieldAt[flightSignal]) {
		cur.fieldAt[flightSignal] = at
		cur.fieldSource[flightSignal] = obs.Source
		cur.InFlight = obs.InFlight
	}
	if obs.BornStuck.Known() && !at.Before(cur.fieldAt[birthSignal]) {
		cur.fieldAt[birthSignal] = at
		cur.fieldSource[birthSignal] = obs.Source
		cur.BornStuck = obs.BornStuck
	}
	if obs.Phase != turnev.PhaseUnknown && !at.Before(cur.fieldAt[phaseSignal]) {
		cur.fieldAt[phaseSignal] = at
		cur.fieldSource[phaseSignal] = obs.Source
		cur.Phase = obs.Phase
	}
	if !obs.LastActivity.IsZero() && obs.LastActivity.After(cur.LastActivity) && !at.Before(cur.fieldAt[activitySignal]) {
		cur.fieldAt[activitySignal] = at
		cur.fieldSource[activitySignal] = obs.Source
		cur.LastActivity = obs.LastActivity
	}
	if obs.QueueDepth != QueueUnknown && !at.Before(cur.fieldAt[queueSignal]) {
		cur.fieldAt[queueSignal] = at
		cur.fieldSource[queueSignal] = obs.Source
		cur.QueueDepth = obs.QueueDepth
	}

	if obs.OwnerQueueDepth != nil && !at.Before(cur.fieldAt[ownerQueueSignal]) {
		cur.OwnerQueueDepth = *obs.OwnerQueueDepth
		cur.fieldAt[ownerQueueSignal] = at
		cur.fieldSource[ownerQueueSignal] = obs.Source
	}

	// An older report never rewinds the clock on what we know. A report at
	// the same instant does update provenance: several sources reporting
	// within one tick are equally current, and the last to speak is the one
	// whose name belongs on the answer.
	if !at.Before(cur.Observed) {
		cur.Observed = at
		cur.Source = obs.Source
	}
	a.seats[obs.Name] = cur
}

// Get returns what is true about a seat.
//
// ok is false for a seat nobody has reported — distinct from a seat we knew
// about and have since lost sight of, which returns ok true with its
// condition fields Unknown and its identity intact. Those two cases call for
// different actions, and collapsing them is 🎯T422's clause 5.
func (a *Authority) Get(name string) (State, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s, ok := a.seats[name]
	if !ok {
		return State{QueueDepth: QueueUnknown}, false
	}
	return a.decay(s), true
}

// Snapshot returns every known seat, by name.
func (a *Authority) Snapshot() []State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]State, 0, len(a.seats))
	for _, s := range a.seats {
		out = append(out, a.decay(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Forget drops a seat entirely — for a name that has been reaped and whose
// identity should not linger as a stale answer.
func (a *Authority) Forget(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.seats, name)
}

// decay is where freshness becomes part of the answer. Condition fields
// expire; identity does not.
func (a *Authority) decay(s State) State {
	now := a.now()
	stale := func(i signal) bool { return s.fieldAt[i].IsZero() || now.Sub(s.fieldAt[i]) > a.stale }
	if stale(aliveSignal) {
		s.Alive = Unknown
	}
	if stale(flightSignal) {
		s.InFlight = Unknown
	}
	if stale(birthSignal) {
		s.BornStuck = Unknown
	}
	if stale(phaseSignal) {
		s.Phase = turnev.PhaseUnknown
	}
	// LastActivity is historical evidence, not a current condition.
	// Lack of later activity does not erase its timestamp.
	if stale(statusSignal) {
		s.Status = ""
	}
	if stale(queueSignal) {
		s.QueueDepth = QueueUnknown
	}
	if stale(resumeSignal) {
		s.ResumeLost = Unknown
	}
	if stale(localAliveSignal) {
		s.LocalAlive = Unknown
	}
	if stale(brokerAliveSignal) {
		s.BrokerAlive = Unknown
	}
	if stale(brokerOwnedSignal) {
		s.BrokerOwned = Unknown
	}
	// A closed local connection cannot contradict a broker that still runs
	// the seat. Both reports live here, so every control gets the same fold.
	if s.LocalAlive == Yes || s.BrokerAlive == Yes {
		s.Alive = Yes
	} else if s.BrokerAlive == No {
		s.Alive = No
	}
	return s
}

// RetractFlight withdraws only a source's own provisional claim. A later
// provider observation is never erased by send bookkeeping catching up.
func (a *Authority) RetractFlight(name, source string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.seats[name]
	if !ok || st.fieldSource[flightSignal] != source {
		return
	}
	st.InFlight = Unknown
	st.fieldAt[flightSignal] = time.Time{}
	a.seats[name] = st
}

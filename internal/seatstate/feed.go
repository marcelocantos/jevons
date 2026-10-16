// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatstate

import (
	"time"

	"github.com/marcelocantos/jevons/internal/turnev"
)

// The feeds (🎯T766.2).
//
// These are the only ways state enters the authority, and each one is a
// translation of something a knowing party already said — not a new
// observation. That constraint is what keeps this package a fold instead of
// becoming the twelfth derivation.
//
// The three feeds correspond to the three things that actually know:
//
//   - claudia reports the seat: alive, in flight, provider, model. It owns
//     the process, so it is the only honest source for those.
//   - the provider's event stream reports motion: a turn began, a turn
//     ended, the seat said something. It is the only source with sub-second
//     resolution, and the only one that is empty when the sink is dark —
//     which the authority must represent as unknown rather than as calm.
//   - the send queue reports its own depth. Nothing else may claim it.
//
// A feed that cannot observe must say nothing rather than report a zero.
// Every function here takes that seriously: there is no path by which a
// failed lookup becomes a fact.

// SeatReport is what claudia says about a seat. It mirrors the fields of
// claudia's AgentInfoResponse that describe condition, and deliberately not
// the ones that describe plumbing (window ids, attach commands, paths).
//
// The value report keeps the fold independent of process handles, so a
// second harness can supply the same observations.
type SeatReport struct {
	Name           string
	SessionID      string
	Provider       string
	Model          string
	Alive          bool
	PromptInFlight bool
	// Known is false when the report itself failed — a broker that did not
	// answer, a seat the daemon could not ask about. A failed lookup is not
	// a dead seat, and this is the field that keeps those apart.
	Known bool
}

// FromClaudia folds a seat report. An unknown report records only that we
// tried: the seat keeps its identity and its condition goes stale on the
// ordinary schedule rather than being overwritten with falsehood.
func (a *Authority) FromClaudia(rep SeatReport, at time.Time) {
	if rep.Name == "" {
		return
	}
	obs := Observation{
		Name:       rep.Name,
		SessionID:  rep.SessionID,
		Provider:   rep.Provider,
		Model:      rep.Model,
		QueueDepth: QueueUnknown,
		Source:     "claudia.report",
		At:         at,
	}
	if rep.Known {
		obs.Alive = TriOf(rep.Alive)
		obs.LocalAlive = TriOf(rep.Alive)
		obs.InFlight = TriOf(rep.PromptInFlight)
		// 🎯T783: the broker's turn-in-flight answer is also phase truth when a
		// turn is running. Leaving Phase alone here is how /api/agents kept
		// phase=idle for five minutes while Migrate refused "turn in flight" —
		// InFlight was Yes from this feed, Phase still Idle from a prior
		// terminal event. Do not force idle from a quiet broker report: the
		// event stream may be a tick ahead and is the only source of sub-second
		// motion; elevating to working when the broker says so is enough.
		if rep.PromptInFlight {
			obs.Phase = turnev.PhaseWorking
		}
	}
	a.Observe(obs)
}

// FromTurnEvent folds one event from the provider's stream.
//
// An event is proof of two things and no more: the seat was alive at that
// moment, and it moved. Whether a turn is now in flight is a question for
// the terminal-stop signal, which is why that is a separate parameter
// rather than inferred from the event's mere existence.
func (a *Authority) FromTurnEvent(name string, terminal bool, at time.Time) {
	if name == "" {
		return
	}
	phase := turnev.PhaseWorking
	inflight := Yes
	if terminal {
		phase = turnev.PhaseIdle
		inflight = No
	}
	a.Observe(Observation{
		Name:         name,
		Alive:        Yes, // it just spoke
		LocalAlive:   Yes,
		Status:       "running",
		InFlight:     inflight,
		Phase:        phase,
		LastActivity: at,
		QueueDepth:   QueueUnknown,
		Source:       "provider.event",
		At:           at,
	})
}

// FromQueue folds the send queue's own depth. depth must be a real count;
// a caller that could not read the queue calls nothing.
func (a *Authority) FromQueue(name string, depth int, at time.Time) {
	if name == "" || depth < 0 {
		return
	}
	a.Observe(Observation{
		Name:       name,
		QueueDepth: depth,
		Source:     "sendq",
		At:         at,
	})
}

// Blocked reports whether a seat is one a control should leave alone, and
// why — the question every loop in the census answers for itself today.
//
// It returns false for unknown as well as for a healthy seat, because
// "leave it alone" and "we cannot see it" are different instructions and a
// caller must be able to tell them apart. Ask [Authority.Get] and read the
// Tri values when the difference matters.
func (a *Authority) Blocked(name string) (reason string, blocked bool) {
	s, ok := a.Get(name)
	if !ok {
		return "", false
	}
	switch {
	case s.Alive == No:
		return "seat is not alive", true
	case s.InFlight == Yes:
		return "a turn is in flight", true
	default:
		return "", false
	}
}

// PhaseAgreesWithFlight reports whether published phase agrees with the
// broker's InFlight answer (🎯T783). Unknown flight is never a disagreement:
// a failure to observe cannot judge. The specimen is phase=idle (or unknown)
// while InFlight=Yes — one of the two is wrong, and the broker is truth.
// InFlight=No with Phase=working is not this target: the event stream may be
// a tick ahead of a quiet broker poll.
func PhaseAgreesWithFlight(phase turnev.Phase, inFlight Tri) bool {
	if inFlight != Yes {
		return true
	}
	return phase == turnev.PhaseWorking
}

// RowPhaseForFlight projects the broker's InFlight onto the phase a fleet
// row must show (🎯T783). When the broker says a turn is running, idle and
// unknown are overwritten; a blocked goal stays blocked (task content, not
// seat turn state). When flight is unknown or no, the prior phase stands.
func RowPhaseForFlight(phase turnev.Phase, inFlight Tri, blocked bool) turnev.Phase {
	if blocked {
		return phase
	}
	if inFlight == Yes {
		return turnev.PhaseWorking
	}
	return phase
}

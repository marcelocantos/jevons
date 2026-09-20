// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package turnrate limits fleet token burn by when the host runs a turn
// (🎯T392.1). Delay, pause, or refuse the next turn; never by the size of
// any one session and never by minting a replacement conversation.
//
// Remint-as-spend-control is withdrawn (🎯T392.1.1 / 🎯T40.2). Session
// identity is T40.2 / T285: a spend path does not remint, rewind, or
// inject a T285 seed. compact-or-rotate is the declined 100k lever; the
// shipped Admit never calls it.
//
// This package is pure on Admit: same request and burn, same decision.
// The Governor holds last-turn times so consecutive admits space.
package turnrate

import (
	"fmt"
	"time"
)

// DefaultMinSpacing is how far apart turns of one agent sit once burn
// has crossed throttle. 30s is a rate limit, not a compact: the session
// stays put and the next turn waits.
const DefaultMinSpacing = 30 * time.Second

// Verdict is what the host does with the next turn.
type Verdict string

const (
	// VerdictAdmit — run this turn now.
	VerdictAdmit Verdict = "admit"
	// VerdictDelay — wait Wait before this turn may run. Session unchanged.
	VerdictDelay Verdict = "delay"
	// VerdictPause — do not run until burn drops. Session unchanged.
	VerdictPause Verdict = "pause"
	// VerdictRefuse — do not run this turn. Session unchanged.
	VerdictRefuse Verdict = "refuse"
)

// Request is one turn asking to run.
type Request struct {
	Agent     string
	SessionID string
	// LastTurn is when this agent last ran a turn. Zero means never.
	LastTurn time.Time
	Now      time.Time
	// OwnerTurn is the owner's own chat, which is not a fleet spend lever.
	OwnerTurn bool
}

// Burn is the live spend picture Admit consults. USD/hour matches the
// 🎯T36 budget.json ladder so the owner retunes one file. Subscription
// accounting (🎯T137) never delays on those figures.
type Burn struct {
	FleetUSDPerHour  float64
	WorkerUSDPerHour float64
	Subscription     bool
}

// Decision is what Admit returns. SessionID is always the request's
// session — never a successor.
type Decision struct {
	Agent     string
	SessionID string
	Verdict   Verdict
	Wait      time.Duration
	Reason    string
}

// Admitted reports whether the turn may run now.
func (d Decision) Admitted() bool { return d.Verdict == VerdictAdmit || d.Verdict == "" }

// Deferred reports a delay, pause, or refuse — the host did not run the turn.
func (d Decision) Deferred() bool {
	switch d.Verdict {
	case VerdictDelay, VerdictPause, VerdictRefuse:
		return true
	default:
		return false
	}
}

// Policy is the turn-rate ladder. Zero USD rungs disable that rung.
// Defaults match cost.DefaultBudgetConfig's worker and fleet ladders.
type Policy struct {
	ThrottleUSDPerHour      float64
	PauseUSDPerHour         float64
	RefuseUSDPerHour        float64
	FleetThrottleUSDPerHour float64
	FleetPauseUSDPerHour    float64
	FleetRefuseUSDPerHour   float64
	MinSpacing              time.Duration
	Disabled                bool
}

// DefaultPolicy is the shipped ladder, aligned with budget.json worker
// (throttle 5 / pause 10 / kill 20 USD/hr) and fleet (10 / 20 / 40).
func DefaultPolicy() Policy {
	return Policy{
		ThrottleUSDPerHour:      5,
		PauseUSDPerHour:         10,
		RefuseUSDPerHour:        20,
		FleetThrottleUSDPerHour: 10,
		FleetPauseUSDPerHour:    20,
		FleetRefuseUSDPerHour:   40,
		MinSpacing:              DefaultMinSpacing,
	}
}

// EffectiveMinSpacing resolves a zero spacing to DefaultMinSpacing.
func (p Policy) EffectiveMinSpacing() time.Duration {
	if p.MinSpacing <= 0 {
		return DefaultMinSpacing
	}
	return p.MinSpacing
}

// Admit decides whether this turn may run. It never changes SessionID
// and never remints.
func (p Policy) Admit(req Request, burn Burn) Decision {
	d := Decision{Agent: req.Agent, SessionID: req.SessionID, Verdict: VerdictAdmit}
	switch {
	case p.Disabled:
		d.Reason = "turn-rate disabled"
		return d
	case req.OwnerTurn:
		d.Reason = "owner turn — not a fleet spend lever"
		return d
	case burn.Subscription:
		d.Reason = "subscription accounting — USD estimates never delay a turn (🎯T137)"
		return d
	}

	switch levelFor(burn.WorkerUSDPerHour, p.RefuseUSDPerHour, p.PauseUSDPerHour, p.ThrottleUSDPerHour) {
	case VerdictRefuse:
		d.Verdict = VerdictRefuse
		d.Reason = fmt.Sprintf("worker burn %.2f USD/hr at refuse — next turn not run; session %s unchanged",
			burn.WorkerUSDPerHour, req.SessionID)
		return d
	case VerdictPause:
		d.Verdict = VerdictPause
		d.Reason = fmt.Sprintf("worker burn %.2f USD/hr at pause — next turn held; session %s unchanged",
			burn.WorkerUSDPerHour, req.SessionID)
		return d
	case VerdictDelay:
		return p.space(req, d, burn.WorkerUSDPerHour, "worker")
	}

	switch levelFor(burn.FleetUSDPerHour, p.FleetRefuseUSDPerHour, p.FleetPauseUSDPerHour, p.FleetThrottleUSDPerHour) {
	case VerdictRefuse:
		d.Verdict = VerdictRefuse
		d.Reason = fmt.Sprintf("fleet burn %.2f USD/hr at refuse — next turn not run; session %s unchanged",
			burn.FleetUSDPerHour, req.SessionID)
		return d
	case VerdictPause:
		d.Verdict = VerdictPause
		d.Reason = fmt.Sprintf("fleet burn %.2f USD/hr at pause — next turn held; session %s unchanged",
			burn.FleetUSDPerHour, req.SessionID)
		return d
	case VerdictDelay:
		return p.space(req, d, burn.FleetUSDPerHour, "fleet")
	}

	d.Reason = "burn within turn-rate ladder"
	return d
}

func (p Policy) space(req Request, d Decision, usdPerHour float64, scope string) Decision {
	spacing := p.EffectiveMinSpacing()
	if req.LastTurn.IsZero() {
		d.Reason = fmt.Sprintf("%s burn %.2f USD/hr at throttle — first turn runs, later turns space by %s",
			scope, usdPerHour, spacing)
		return d
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	age := now.Sub(req.LastTurn)
	if age >= spacing {
		d.Reason = fmt.Sprintf("%s burn %.2f USD/hr at throttle — spacing elapsed, run this turn",
			scope, usdPerHour)
		return d
	}
	d.Verdict = VerdictDelay
	d.Wait = spacing - age
	d.Reason = fmt.Sprintf("%s burn %.2f USD/hr at throttle — next turn in %s; session %s unchanged",
		scope, usdPerHour, d.Wait.Round(time.Millisecond), req.SessionID)
	return d
}

func levelFor(rate, refuse, pause, throttle float64) Verdict {
	switch {
	case refuse > 0 && rate >= refuse:
		return VerdictRefuse
	case pause > 0 && rate >= pause:
		return VerdictPause
	case throttle > 0 && rate >= throttle:
		return VerdictDelay
	default:
		return VerdictAdmit
	}
}

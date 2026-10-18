// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package escalate fits a 🎯T899 escalation ladder to the seat it is offered
// to, and reads the refusal that says a ladder never started (🎯T931).
//
// On 2026-09-29 every owner message to a busy Claude Code worker failed with
// 502 client_bug: the ladder's first rung is steer, Claude Code in tmux has
// no steer mechanism, and the refusal came back through the broker as
// "broker protocol: agent_failed: steer unsupported: …" — a string, so the
// errors.Is(err, claudia.ErrSteerUnsupported) that was meant to fall back to
// the ordinary queue never matched. Both halves are fixed here: the ladder is
// built only from rungs the seat reports it can run, and a refusal that does
// arrive is recognised in its relayed form too.
package escalate

import (
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

// capsReader is a seat that reports what it can do to an open turn. A
// broker-held claudia.Agent answers for the seat the daemon actually runs.
type capsReader interface {
	TurnCaps() claudia.TurnCaps
}

// CapsOf returns seat's turn capabilities, or ok=false when it cannot say.
func CapsOf(seat any) (claudia.TurnCaps, bool) {
	c, ok := seat.(capsReader)
	if !ok || c == nil {
		return claudia.TurnCaps{}, false
	}
	return c.TurnCaps(), true
}

// Fit returns ladder cut down to the rungs caps can run. A ladder whose first
// rung is steer cannot start on a seat with no steer mechanism, so Fit
// returns nil and the caller holds the message for the turn boundary, as it
// does for a sender with no ladder. Interrupt rungs are dropped from a seat
// that cannot interrupt.
func Fit(ladder Ladder, caps claudia.TurnCaps) Ladder {
	if len(ladder) == 0 || (ladder[0].Mode == claudia.DeliverySteer && !caps.CanSteer) {
		return nil
	}
	fitted := Ladder{ladder[0]}
	for _, step := range ladder[1:] {
		if step.Mode == claudia.DeliveryInterrupt && !caps.CanInterrupt {
			continue
		}
		fitted = append(fitted, step)
	}
	return fitted
}

// SteerUnsupported reports claudia's typed steer refusal. The broker carries
// the same cause on the relayed error. A rendered sentence is not a match.
func SteerUnsupported(err error) bool {
	return agenterr.ProviderCause(err) == agenterr.CauseSteerUnsupported
}

// NotStarted reports whether err says the ladder never reached the seat: the
// seat cannot steer, or it had no turn open to steer into. The ordinary path
// then holds or submits the message. Both facts are causes. A sentence that
// quotes them is not.
func NotStarted(err error) bool {
	switch agenterr.ProviderCause(err) {
	case agenterr.CauseSteerUnsupported, agenterr.CauseTurnIdle:
		return true
	default:
		return false
	}
}

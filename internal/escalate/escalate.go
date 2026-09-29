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
	"errors"
	"strings"

	"github.com/marcelocantos/claudia"
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
func Fit(ladder claudia.Escalation, caps claudia.TurnCaps) claudia.Escalation {
	if len(ladder) == 0 || (ladder[0].Mode == claudia.DeliverySteer && !caps.CanSteer) {
		return nil
	}
	fitted := claudia.Escalation{ladder[0]}
	for _, step := range ladder[1:] {
		if step.Mode == claudia.DeliveryInterrupt && !caps.CanInterrupt {
			continue
		}
		fitted = append(fitted, step)
	}
	return fitted
}

// brokerAgentFailedPrefix is how claudia's broker renders a seat failure it
// relays: ProtocolError.Error() with Code agent_failed and no Field. What
// follows is the seat's own error text, untouched.
const brokerAgentFailedPrefix = "broker protocol: agent_failed: "

// NotStarted reports whether err says the ladder never reached the seat: the
// seat cannot steer, or it had no turn open to steer into. The ordinary path
// then holds or submits the message. A broker-held seat relays these as text,
// so the relayed envelope is decoded and its message compared against the
// sentinels' own wording; a string that merely mentions them is not a match.
func NotStarted(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, claudia.ErrSteerUnsupported) || errors.Is(err, claudia.ErrTurnIdle) {
		return true
	}
	msg, ok := strings.CutPrefix(err.Error(), brokerAgentFailedPrefix)
	if !ok {
		return false
	}
	unsupported := claudia.ErrSteerUnsupported.Error()
	return msg == unsupported || strings.HasPrefix(msg, unsupported+": ") ||
		msg == claudia.ErrTurnIdle.Error()
}

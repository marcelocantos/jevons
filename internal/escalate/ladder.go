// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package escalate

import (
	"github.com/marcelocantos/claudia"
)

// Step is one rung of an escalation ladder. Mode is what to try; After
// is how long a later rung waits. Step and Ladder are now aliases for
// claudia.EscalationStep and claudia.Escalation (🎯T1008.2): the shapes
// were always identical, and *claudia.Agent.SendEscalating actually
// climbs the ladder against a live turn (absorb detection, cancellation
// on supersede), where jevons's own Send below only ever fired the
// first rung. escalate.Fit/CapsOf/NotStarted remain jevons-only policy
// on top of this shape, with no claudia equivalent.
type Step = claudia.EscalationStep

// Ladder is an ordered list of rungs. A nil ladder means the sender
// waits for the turn boundary.
type Ladder = claudia.Escalation

// Handle adapts a published *claudia.Agent to the ladder interface.
// Since Step/Ladder are now aliases of claudia.EscalationStep/Escalation,
// *claudia.Agent already satisfies escalatingSender/overseerEscalator
// directly via its own SendEscalating; Handle is kept only for callers
// that still hold a seat typed as something other than *claudia.Agent
// (none remain in production code as of 🎯T1008.2, but tests exercise
// the shape via their own fakes).
type Handle struct {
	Agent *claudia.Agent
}

func (h Handle) TurnCaps() claudia.TurnCaps {
	if h.Agent == nil {
		return claudia.TurnCaps{}
	}
	return h.Agent.TurnCaps()
}

func (h Handle) SendEscalating(text string, ladder Ladder) (claudia.DeliveryOutcome, error) {
	if h.Agent == nil {
		return claudia.DeliveryOutcome{}, claudia.ErrTurnIdle
	}
	return h.Agent.SendEscalating(text, ladder)
}

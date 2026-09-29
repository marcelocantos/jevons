// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package escalate

import (
	"fmt"
	"time"

	"github.com/marcelocantos/claudia"
)

// Step is one rung of an escalation ladder. Mode is what to try; After
// is how long a later rung waits. The published claudia module does not
// export this type; Jevons owns the ladder and runs the first rung
// through Agent.SendMode.
type Step struct {
	Mode  claudia.DeliveryMode
	After time.Duration
}

// Ladder is an ordered list of rungs. A nil ladder means the sender
// waits for the turn boundary.
type Ladder []Step

// Send runs the first rung on seat. Later rungs (interrupt after a
// deadline) are host policy the caller reports; the published
// Agent.SendMode does not schedule them.
func Send(seat interface {
	SendMode(string, claudia.DeliveryMode) (claudia.DeliveryOutcome, error)
}, text string, ladder Ladder) (claudia.DeliveryOutcome, error) {
	if seat == nil || len(ladder) == 0 {
		return claudia.DeliveryOutcome{}, fmt.Errorf("empty escalation ladder")
	}
	return seat.SendMode(text, ladder[0].Mode)
}

// Handle adapts a published *claudia.Agent to the ladder interface.
// The pinned module has no Agent.SendEscalating.
type Handle struct {
	Agent *claudia.Agent
}

func (h Handle) Alive() bool {
	return h.Agent != nil && h.Agent.Alive()
}

func (h Handle) TurnPhase() claudia.TurnPhase {
	if h.Agent == nil {
		return claudia.TurnIdle
	}
	return h.Agent.TurnPhase()
}

func (h Handle) TurnCaps() claudia.TurnCaps {
	if h.Agent == nil {
		return claudia.TurnCaps{}
	}
	return h.Agent.TurnCaps()
}

func (h Handle) SendEscalating(text string, ladder Ladder) (claudia.DeliveryOutcome, error) {
	return Send(h.Agent, text, ladder)
}

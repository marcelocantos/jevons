// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package escalate

import (
	"fmt"
	"time"

	"github.com/marcelocantos/claudia"
)

// Step is one rung of an escalation ladder. Mode is what to try; After
// is how long a later rung waits. Claudia's pinned module (since
// v0.51.0) now exports the same shape as claudia.EscalationStep plus
// *claudia.Agent.SendEscalating, which actually climbs the ladder
// against a live turn (absorb detection, cancellation on supersede).
// Jevons's own Step/Ladder/Send/Handle predate that export and are
// kept only because escalate.Fit/CapsOf/NotStarted layer host-specific
// policy on this shape across many call sites; migrating callers onto
// claudia.Escalation/Agent.SendEscalating directly is tracked
// separately (🎯T1008.1) rather than folded into this change.
type Step struct {
	Mode  claudia.DeliveryMode
	After time.Duration
}

// Ladder is an ordered list of rungs. A nil ladder means the sender
// waits for the turn boundary.
type Ladder []Step

// Send runs the first rung on seat. Later rungs (interrupt after a
// deadline) are host policy the caller reports; this does not climb
// the ladder itself. claudia.Agent.SendEscalating now does climb a
// claudia.Escalation ladder against a live turn (🎯T1008.1 tracks
// moving callers onto it).
func Send(seat interface {
	SendMode(string, claudia.DeliveryMode) (claudia.DeliveryOutcome, error)
}, text string, ladder Ladder) (claudia.DeliveryOutcome, error) {
	if seat == nil || len(ladder) == 0 {
		return claudia.DeliveryOutcome{}, fmt.Errorf("empty escalation ladder")
	}
	return seat.SendMode(text, ladder[0].Mode)
}

// Handle adapts a published *claudia.Agent to the ladder interface.
// claudia.Agent now has its own SendEscalating over claudia.Escalation
// (🎯T1008.1 tracks moving this handle's callers onto it directly).
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
	return Send(h.Agent, text, ladder)
}

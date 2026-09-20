// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package delivery names the owner's delivery intent for one user-text send
// and the mechanism the daemon actually ran (🎯T657). It mirrors the claudia
// broker send.mode wire (../claudia/docs/design/steer-interrupt-turn-api.md)
// without importing claudia, so the HTTP server, the mux and the MCP layer
// share one vocabulary and the pinned claudia release need not carry it.
package delivery

import "fmt"

// Mode is the host intent for one send.
type Mode string

const (
	// ModeSubmit starts a turn when the seat is idle; the daemon queues when busy.
	ModeSubmit Mode = "submit"
	// ModeSteer folds the text into the running turn (plain submit when idle).
	ModeSteer Mode = "steer"
	// ModeInterrupt cancels the open turn, then submits.
	ModeInterrupt Mode = "interrupt"
	// ModeQueue holds the text for the next turn boundary without touching the seat.
	ModeQueue Mode = "queue"
)

// Modes lists every mode in wire order.
func Modes() []Mode { return []Mode{ModeSubmit, ModeSteer, ModeInterrupt, ModeQueue} }

// Mechanisms recorded on the status event. Spellings follow claudia's
// DeliveryOutcome.Mechanism so a mechanism the broker returned and one the
// jevons daemon chose itself read the same in a log.
const (
	// MechanismSubmit: the text became a new turn.
	MechanismSubmit = "submit"
	// MechanismInterruptThenSubmit: the open turn was cancelled, then the text submitted.
	MechanismInterruptThenSubmit = "interrupt+submit"
	// MechanismClientQueue: the daemon holds the text for the next turn boundary.
	MechanismClientQueue = "client_queue"
	// MechanismQueueUntilIdle: steer was asked for but this seat (or this
	// claudia) cannot steer, so the text is held honestly — never reported as steered.
	MechanismQueueUntilIdle = "queue_until_idle"
	// MechanismSessionCancelPrompt: the jevons daemon itself cancelled the
	// open turn (Interrupt) and then submitted (Send), because the seat's
	// process exposes no SendMode. Distinct from claudia's own
	// interrupt+submit so a log tells the two apart.
	MechanismSessionCancelPrompt = "session_cancel+prompt"
)

// Turn phases as claudia's SendMode reports them in DeliveryOutcome.PhaseBefore.
// Read through the seam as plain strings (🎯T448: the pin has no TurnPhase).
const (
	PhaseIdle   = "idle"
	PhaseInTurn = "in_turn"
)

// Parse normalises a wire mode. Empty is submit. interruptAlias is the
// deprecated `interrupt=true` flag: it stands for mode=interrupt when no
// explicit mode is given, and is refused when it contradicts one.
func Parse(raw string, interruptAlias bool) (Mode, error) {
	switch Mode(raw) {
	case "":
		if interruptAlias {
			return ModeInterrupt, nil
		}
		return ModeSubmit, nil
	case ModeSubmit, ModeSteer, ModeQueue:
		if interruptAlias {
			return "", fmt.Errorf("mode %q contradicts interrupt=true (the flag is a deprecated alias for mode=interrupt)", raw)
		}
		return Mode(raw), nil
	case ModeInterrupt:
		return ModeInterrupt, nil
	default:
		return "", fmt.Errorf("mode %q is not one of submit, steer, interrupt, queue", raw)
	}
}

// Interrupts reports whether the mode cancels an open turn before sending.
func (m Mode) Interrupts() bool { return m == ModeInterrupt }

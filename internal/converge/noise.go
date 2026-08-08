// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"errors"
	"fmt"
	"strings"
)

// Actuation seam for the 🎯T317 ladder. The ladder decides which rung a gap
// has earned; this file decides what each rung *says* and hands it to a sink
// the daemon implements. Everything here stays pure — the sinks are the only
// impure edge, so the whole ladder including its wording is hermetic.
//
// The overseer rung is an event_push and nothing more. It informs the root
// overseer that a gap is stuck; it does not mark the gap satisfied, does not
// remove it from the reconcile set, and does not stop the ladder from
// climbing to the human rung if the noise fails to produce work.

// Event classes for the overseer rung (🎯T317 acceptance 1).
const (
	// EventClassImpatient is the first shout about a gap: re-pressure did not
	// bring the agent back to working.
	EventClassImpatient = "converge-impatient"
	// EventClassFailing is every shout after that, and any shout once the
	// owner has been alerted: the noise itself is now failing.
	EventClassFailing = "converge-failing"
)

// eventClass escalates the class the second time one gap reaches the overseer
// rung, so the overseer can tell "stuck" from "still stuck after I was told".
func eventClass(r Rung, st *agentState) string {
	if r != RungOverseerNoise {
		return ""
	}
	for _, prior := range st.rungs[:len(st.rungs)-1] {
		if prior == RungOverseerNoise || prior == RungHumanAlert {
			return EventClassFailing
		}
	}
	return EventClassImpatient
}

// OverseerSink pushes an event to the root overseer. Implemented by the
// daemon over jevons_event_push; the pure layer only decides class and text.
type OverseerSink interface {
	PushConvergeEvent(class, text string) error
}

// HumanSink raises and withdraws owner-visible sticky chrome — daily chat
// chrome, an OS notification, or both. Withdrawal is driven by satisfaction,
// never by an ack requirement (🎯T319).
type HumanSink interface {
	RaiseImpatienceAlert(agent, text string) error
	ClearImpatienceAlert(agent string) error
}

// RePressureSink re-pressures the agent itself — the 🎯T315 actuator.
type RePressureSink interface {
	RePressure(agent, mission string) error
}

// Sinks is the set of actuators available this tick. Any may be nil: a nil
// sink means that rung cannot fire yet, which Actuate reports as an error
// rather than silently swallowing (no silent-fail sends).
type Sinks struct {
	RePressure RePressureSink
	Overseer   OverseerSink
	Human      HumanSink
}

// Actuate applies one tick's actions through the sinks and returns the errors
// it hit, one per failed action. It never decides policy and never touches
// ladder state: a send that fails is simply noise that did not land, and the
// gap stays unsatisfied either way, so the next tick will try again under the
// same anti-thrash interval.
func Actuate(actions []Action, s Sinks) []error {
	var errs []error
	for _, a := range actions {
		if err := actuateOne(a, s); err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", a.Rung, a.Agent, err))
		}
	}
	return errs
}

var errNoSink = errors.New("no sink wired for this rung")

func actuateOne(a Action, s Sinks) error {
	if a.Kind == ActClearHuman {
		if s.Human == nil {
			return errNoSink
		}
		return s.Human.ClearImpatienceAlert(a.Agent)
	}
	switch a.Rung {
	case RungRepressure:
		if s.RePressure == nil {
			return errNoSink
		}
		return s.RePressure.RePressure(a.Agent, a.Mission)
	case RungOverseerNoise:
		if s.Overseer == nil {
			return errNoSink
		}
		return s.Overseer.PushConvergeEvent(a.Class, RenderOverseerNoise(a))
	case RungHumanAlert:
		if s.Human == nil {
			return errNoSink
		}
		return s.Human.RaiseImpatienceAlert(a.Agent, RenderHumanAlert(a))
	}
	return nil
}

// RenderOverseerNoise is what the root overseer reads. It states the gap, the
// dwell, and — load-bearing — that being told is not satisfaction, so the
// overseer does not close the loop by acknowledging it.
func RenderOverseerNoise(a Action) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Convergence impatience — %s**", a.Agent)
	if a.Mission != "" {
		fmt.Fprintf(&b, " on %s", a.Mission)
	}
	fmt.Fprintf(&b, "\n- Open mission, agent not working, for %s.\n", humanDwell(a.Dwell))
	fmt.Fprintf(&b, "- %s\n", a.Reason)
	b.WriteString("- This event is a noise step, not satisfaction: the gap stays on the reconcile set until the agent is working or the mission closes. Acknowledging this does not clear it.")
	return b.String()
}

// RenderHumanAlert is what the owner reads on the sticky. It says plainly
// that no ack is needed — satisfaction withdraws it (🎯T319).
func RenderHumanAlert(a Action) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s is stuck**", a.Agent)
	if a.Mission != "" {
		fmt.Fprintf(&b, " on %s", a.Mission)
	}
	fmt.Fprintf(&b, " — open mission, not working, for %s.\n", humanDwell(a.Dwell))
	b.WriteString("Re-pressure and overseer noise both failed to produce work. ")
	b.WriteString("This alert withdraws itself as soon as the agent is working again — you do not need to dismiss it.")
	return b.String()
}

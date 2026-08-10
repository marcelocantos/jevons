// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// 🎯T416 — what a send DID, in four answers rather than two.
//
// The old send path had two: the call returned an error, or it did not, and
// "did not" was reported as "Message sent". On a tmux Claude agent that single
// answer covers three genuinely different fates, and the daemon could not tell
// them apart because it never looked past its own return value:
//
//	(a) the paste was submitted and a turn began         — a real send
//	(b) the CLI took it behind a live turn, to run later — legitimate, not begun
//	(c) it landed in the composer and stayed there       — the defect
//
// Wrapping every send in a turn-begin confirmation, the way the spawn path is
// wrapped since 🎯T387, gets (a) and (c) right and calls (b) a failure. That is
// not a small over-strictness: a fresh spawn is never busy, so (b) barely
// exists on the start path, while on the send path it is ordinary — every
// reply to a working agent is (b). Failing those would strand the fleet, which
// is the hazard 🎯T387's own residual names and clause 9 pins as a mutant.
//
// So the daemon has to know whether a turn was already in flight. It tracks
// that per agent, in this process, from the terminal stops it already observes
// for the send queue and the reply notify.
//
// WHICH MAKES A FOURTH ANSWER MANDATORY, and this is the part that is easy to
// get wrong. In-process state does not survive a daemon restart, and jevonsd
// restarted three times on the day this was written. After a restart, an agent
// that IS mid-turn has no in-flight record — so a three-state discriminator
// falls through to "no turn in flight and no evidence", and reports
// pasted-but-never-submitted about a perfectly healthy send. That is a false
// accusation of the exact defect being fixed, on the most common path there
// is. A control describing its own bookkeeping rather than the world is the
// shape this whole family of bugs is made of; adding one more instance of it
// while fixing one would be its own joke.
//
// Hence FlightUnknown is a first-class answer, not a default that collapses
// into one of the others. The daemon leaves an agent unknown until it has
// itself observed a boundary: it does not infer idleness from having a
// registry row, from the process being alive, or from having launched
// something at boot — a pane can outlive the daemon that started it, and the
// warm pool hands out windows this process never watched.
//
// Unknown is cheap in practice because the payload witness answers most of it
// anyway: if the message shows up in the transcript, it was submitted, and it
// no longer matters what the agent was doing beforehand. Unknown only decides
// the cases where the payload never appears.
//
// And even there it is narrower than it looks, because the born-stuck case —
// the one that actually hurts, an auto-spawned worker sitting on an unsubmitted
// brief with nobody watching a pane — is decided positively without any flight
// record at all. A session with no transcript file has never submitted
// anything, so no turn can be in flight for the payload to be waiting behind
// (TurnEvidence.TranscriptAbsent). What is left as genuinely unknown is a
// LIVE session whose transcript exists and moves but has not yet shown this
// payload, and there "I cannot tell whether this is queued or stuck" is the
// true answer — 🎯T417's mid-turn read is the same ambiguity from the other
// side.

// TurnFlight is what this daemon process knows about whether an agent has a
// turn running. Not what is true — what is KNOWN, which is the distinction the
// zero value has to carry.
type TurnFlight int

const (
	// FlightUnknown: no observed boundary for this agent in this process.
	// The zero value, deliberately: a fact the daemon has not established
	// must never start life as a claim.
	FlightUnknown TurnFlight = iota
	// FlightIdle: a turn was observed ending, and nothing has been sent since.
	FlightIdle
	// FlightInFlight: a send was confirmed begun, and no terminal stop since.
	FlightInFlight
)

func (f TurnFlight) String() string {
	switch f {
	case FlightIdle:
		return "idle"
	case FlightInFlight:
		return "in_flight"
	default:
		return "unknown"
	}
}

// SendOutcome is what the daemon is willing to say about a send once it has
// looked at the agent. These are answers, not statuses: the wire status
// strings are derived from them in deliverToSender.
type SendOutcome string

const (
	// OutcomeBegun: this payload became a turn.
	OutcomeBegun SendOutcome = "begun"
	// OutcomeQueuedBehindTurn: a turn was already running, so the message is
	// held for delivery when it ends. Not begun, not lost, and not a failure.
	OutcomeQueuedBehindTurn SendOutcome = "queued_behind_turn"
	// OutcomeUnconfirmed: the send call succeeded, the payload has not
	// appeared, and the daemon does not know whether a turn was already
	// running. Honest ignorance — never reported as sent, never as a defect.
	OutcomeUnconfirmed SendOutcome = "delivered_unconfirmed"
	// OutcomeNotSubmitted: the agent was known idle and the payload never
	// arrived. Pasted and left sitting — the defect this target is about.
	OutcomeNotSubmitted SendOutcome = "not_submitted"
)

// ClassifySendOutcome decides what a send did. Pure: the whole discriminator
// is here so it can be exercised without a process, a transcript, or a clock.
//
// ev.Durable says the agent keeps a transcript the daemon can read (a
// Claude-shaped backend), and it comes from the instrument that looked rather
// than from a second lookup. It decides what counts as proof:
//
//   - durable: the payload must be IN the transcript. Growth is not enough —
//     a send's Enter submits the previous backlog, so growth routinely arrives
//     carrying somebody else's message (🎯T416 specimen A).
//   - live-stream (Grok ACP): there is no transcript to read, so a session
//     event remains the best available evidence, exactly as on the spawn path.
//     Holding out for a payload match here would refuse every Grok send.
func ClassifySendOutcome(flight TurnFlight, ev TurnEvidence) SendOutcome {
	if ev.PayloadSeen {
		// Identifies the message itself. Nothing the daemon believes about
		// the agent's state can outrank seeing the payload arrive.
		return OutcomeBegun
	}
	if !ev.Observed {
		// No instrument ran — no process to watch. "I did not look" is not a
		// finding, and dressing it as one would accuse a healthy send of the
		// very defect this target exists to report.
		return OutcomeUnconfirmed
	}
	if ev.TranscriptAbsent {
		// The one positive born-stuck test, and it outranks flight because it
		// is an observation of the agent while flight is this process's own
		// bookkeeping. A turn in flight would have created the transcript by
		// submitting the message that started it; no file means no turn has
		// ever begun here, so there is nothing for this payload to be waiting
		// behind. This is what rescues the unknown case after a restart —
		// without it, born-stuck and "I have forgotten what this agent was
		// doing" are the same answer, and the born-stuck agent stays silent.
		return OutcomeNotSubmitted
	}
	switch flight {
	case FlightInFlight:
		return OutcomeQueuedBehindTurn
	case FlightIdle:
		if !ev.Durable && ev.SessionEvent {
			return OutcomeBegun
		}
		return OutcomeNotSubmitted
	default:
		return OutcomeUnconfirmed
	}
}

// flightState reports what is known about name's turn.
func (s *Server) flightState(name string) TurnFlight {
	if s == nil || strings.TrimSpace(name) == "" {
		return FlightUnknown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentFlight[name]
}

// noteTurnInFlight records a turn started by a send this daemon confirmed.
func (s *Server) noteTurnInFlight(name string) { s.setFlight(name, FlightInFlight) }

// noteTurnEnded records an observed terminal stop. Called from the agent event
// sink, which is the same signal that drains the send queue and delivers the
// reply — if it were unreliable, worker replies would not arrive either.
func (s *Server) noteTurnEnded(name string) { s.setFlight(name, FlightIdle) }

// Forgetting is clearAgentTurnBegan's job: a seat torn down drops its
// turn-began mark and its flight record together, since both are claims about
// an agent that no longer exists.

func (s *Server) setFlight(name string, f TurnFlight) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentFlight == nil {
		s.agentFlight = map[string]TurnFlight{}
	}
	s.agentFlight[name] = f
}

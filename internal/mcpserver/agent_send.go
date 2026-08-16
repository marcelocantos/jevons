// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// agentSendResult is the outcome of sendToAgent (🎯T111.1).
type agentSendResult struct {
	// Status: sent | queued | interrupted_sent | interrupted_queued | rehydrated_sent
	Status  string
	Message string
	Queued  int // pending after this call (including this message if queued)
}

// agentSender is the process surface sendToAgent needs (testable).
type agentSender interface {
	Send(text string) error
	Interrupt() error
	Alive() bool
}

// isPromptInFlight reports a concurrent prompt/turn (busy) so senders
// queue or interrupt instead of hard-failing (🎯T111.1, 🎯T214 J6).
// Provider-agnostic: not Grok ACP string only — see agenterr.IsPromptBusy.
func isPromptInFlight(err error) bool {
	return agenterr.IsPromptBusy(err)
}

// enqueueAgentSend appends text for delivery after the current turn.
// Returns total pending for name.
func (s *Server) enqueueAgentSend(name, text string) (int, error) {
	_, depth, err := s.sendQueue().Append(name, text, time.Now())
	return depth, err
}

// dequeueAgentSend pops the oldest pending message, or "" if empty.
func (s *Server) dequeueAgentSend(name string) string {
	e, ok, err := s.sendQueue().PopFront(name)
	if err != nil || !ok {
		return ""
	}
	return e.Text
}

// pendingAgentSends returns queue depth for name.
func (s *Server) pendingAgentSends(name string) int {
	n, err := s.sendQueue().Depth(name)
	if err != nil {
		return 0
	}
	return n
}

// AgentDeliverResult is the public outcome of DeliverAgentMessage (🎯T275).
// Status matches MCP jevons_agent_send: sent | queued | interrupted_sent |
// interrupted_queued | rehydrated_sent.
type AgentDeliverResult struct {
	Status  string
	Message string
	Queued  int
}

// DeliverAgentMessage is the product deliver path shared by HTTP
// POST /api/agents/{name}/send and MCP jevons_agent_send (🎯T275 / 🎯T111.1).
// When a prompt is already in flight and interrupt is false, the message is
// queued for after the turn — not a 409 silent dead-end. Does not inject the
// fleet standing brief (MCP handleAgentSend applies EnsureFleetBrief first).
// Owner origin by default; DeliverAgentMessageAs carries an explicit one.
func (s *Server) DeliverAgentMessage(name, text string, interrupt bool) (AgentDeliverResult, error) {
	return s.DeliverAgentMessageAs(name, text, OriginOwner, interrupt)
}

// DeliverAgentMessageAs is the origin-carrying form, and the entry point the
// HTTP send handler uses so owner↔agent and agent↔agent traffic share one
// implementation addressed by name (🎯T309.3).
func (s *Server) DeliverAgentMessageAs(name, text string, origin SendOrigin, interrupt bool) (AgentDeliverResult, error) {
	res, err := s.deliverByName(name, text, origin, interrupt)
	if err != nil {
		return AgentDeliverResult{}, err
	}
	return AgentDeliverResult{
		Status:  res.Status,
		Message: res.Message,
		Queued:  res.Queued,
	}, nil
}

// sendToAgent rehydrates if needed, optionally interrupts a busy turn,
// sends text, or queues when the prompt is already in flight (🎯T111.1).
// Does not inject the fleet standing brief — callers that need it
// (handleAgentSend) apply EnsureFleetBrief first.
//
// 🎯T309.3: a shim over deliverByName, which also resolves the overseer by
// name. Daemon-internal callers (worker-idle, daemon-restarted, RSI coach,
// fleet health) speak as the owner surface with agent origin; MCP fleet
// callers use sendToAgentAs so lineage names the real agent (🎯T321).
// The owner's own turns arrive through DeliverAgentMessageAs.
func (s *Server) sendToAgent(name, text string, interrupt bool) (agentSendResult, error) {
	return s.deliverByName(name, text, OriginAgent, interrupt)
}

// sendToAgentAs is the MCP fleet form of sendToAgent: same busy/queue path
// and agent origin, but the caller is named so AuthorizeDeliver can decide
// (and log denials with actor + relation) per-caller (🎯T321).
func (s *Server) sendToAgentAs(actor, name, text string, interrupt bool) (agentSendResult, error) {
	return s.deliverByNameAs(actor, name, text, OriginAgent, interrupt)
}

// ensureAgentProcess returns a live process, rehydrating when registered
// but stopped/dead.
func (s *Server) ensureAgentProcess(name string) (*claudia.Agent, bool, error) {
	if s.registry != nil {
		if reps := SweepDeadAgents(s.registry, s.overseerName(), s.fleetIntent()); len(reps) > 0 {
			line := FormatDeadAgentReport(reps)
			slog.Info(line)
			s.notifyFleetHealth(line)
		}
	}

	proc := s.registry.Get(name)
	if proc != nil && proc.Alive() {
		return proc, false, nil
	}
	if s.registry.Def(name) == nil {
		return nil, false, fmt.Errorf("agent %q is not running", name)
	}
	// 🎯T414 / 🎯T408: not-running is a fact, never a licence. Everything
	// below this point starts a process, and it used to run on the strength
	// of the process being absent — which is how a deliberate stop failed to
	// survive a delivery. A message to a stood-down agent is not itself
	// authority to stand it back up; lifting the intent is.
	if dec := s.AllowFleetControl(name, fleetintent.ControlDeliverStart); !dec.Allow {
		return nil, false, fmt.Errorf("agent %q is not running and intent says it should not be: %s (%s) — lift the intent to resume",
			name, fleetintent.Describe(dec.Blocking), dec.Reason)
	}

	// 🎯T409: the same lost-session recovery agent_start performs (🎯T313),
	// on the path that actually needs it. Launch fails closed when the row
	// is Materialized and the transcript is gone, and this path is reached
	// by the impatience ladder — which retries on a timer, hits the
	// identical error every time, and never escapes.
	//
	// Observed 2026-08-10: 10 of 16 agents carried a session id with no
	// file on disk after a token-exhaustion event, because Materialized is
	// set at launch while the transcript only appears once a session
	// produces a turn. Every repressure of those agents failed forever.
	if lost, ok, err := fleet.RehydrateLostSessionIn(s.registry, name); err != nil {
		slog.Warn("lost-session rehydrate failed; falling through to launch",
			"name", name, "err", err)
	} else if ok {
		slog.Info("agent send rehydrated lost session", "name", name, "detail", lost.Describe())
	}

	p2, err := s.registry.Launch(name)
	if err != nil {
		return nil, false, fmt.Errorf("agent %q is not running and rehydrate failed: %v", name, err)
	}
	s.wireAgentEvents(name, p2)
	slog.Info("agent send rehydrated dead/stopped process", "name", name)
	return p2, true, nil
}

// logAgentSendResult emits structured slog for busy/queue/interrupt outcomes
// (🎯T120.2). Shared field schema: component, name, status, queued, rehydrated.
func logAgentSendResult(name string, res agentSendResult, rehydrated bool) {
	slog.Info("agent_send",
		"component", "agent_send",
		"name", name,
		"status", res.Status,
		"queued", res.Queued,
		"rehydrated", rehydrated,
	)
}

// logAgentSendOutcome is logAgentSendResult plus what the daemon actually
// observed (🎯T416). status=sent was the log line that made this defect
// invisible for thirteen hours, so the record now carries the evidence the
// status was derived from — an operator reading logs can tell a confirmed
// delivery from an unconfirmed one without re-deriving it from a transcript.
func logAgentSendOutcome(name string, res agentSendResult, rehydrated bool, outcome SendOutcome, flight TurnFlight, ev TurnEvidence) {
	slog.Info("agent_send",
		"component", "agent_send",
		"name", name,
		"status", res.Status,
		"queued", res.Queued,
		"rehydrated", rehydrated,
		"outcome", string(outcome),
		"flight", flight.String(),
		"payload_seen", ev.PayloadSeen,
		"evidence", ev.Detail,
	)
}

// reportSendOutcome turns an outcome into what the caller is told, and is the
// single place the send path is allowed to claim delivery (🎯T416).
//
// interrupted distinguishes the two wire statuses for a begun turn; rehydrated
// the two for a fresh process. Neither changes the decision — only its name.
func (s *Server) reportSendOutcome(name string, outcome SendOutcome, flight TurnFlight, ev TurnEvidence, rehydrated, interrupted bool) (agentSendResult, error) {
	switch outcome {
	case OutcomeBegun:
		msg := fmt.Sprintf("Message sent to %q. You will be notified when it responds.", name)
		switch {
		case interrupted:
			msg = fmt.Sprintf("Interrupted in-flight turn on %q and sent the new message.", name)
		case rehydrated:
			msg += " (rehydrated after dead/stopped process)"
		}
		res := agentSendResult{Status: sentStatus(rehydrated, interrupted), Message: msg, Queued: s.pendingAgentSends(name)}
		logAgentSendOutcome(name, res, rehydrated, outcome, flight, ev)
		// 🎯T305: never_briefed → running. Now earned from the payload
		// arriving rather than from the send call returning.
		s.markAgentTurnBegan(name)
		s.noteTurnInFlight(name)
		return res, nil

	case OutcomeUnconfirmed:
		// The one answer that exists because state can be lost. Not success:
		// the caller must not read this as delivered. Not a defect either:
		// after a restart the daemon has no record of a turn it never saw
		// start, and a healthy send to a busy agent looks exactly like this.
		res := agentSendResult{
			Status: "delivered_unconfirmed",
			Message: fmt.Sprintf(
				"Message handed to %q but NOT confirmed as a turn: %s. "+
					"This daemon has no record of whether %[1]q already had a turn running "+
					"(that record does not survive a restart), so it cannot tell a message waiting "+
					"behind a live turn from one left sitting in the composer. Treat as undelivered "+
					"until the agent responds; confirm by reading its transcript for the payload.",
				name, evidenceDetail(ev)),
			Queued: s.pendingAgentSends(name),
		}
		logAgentSendOutcome(name, res, rehydrated, outcome, flight, ev)
		return res, nil

	case OutcomeQueuedBehindTurn:
		res := agentSendResult{
			Status: "queued",
			Message: fmt.Sprintf(
				"busy: %q had a turn in flight; message queued (%d pending) for delivery when it ends.",
				name, s.pendingAgentSends(name)),
			Queued: s.pendingAgentSends(name),
		}
		logAgentSendOutcome(name, res, rehydrated, outcome, flight, ev)
		return res, nil

	default: // OutcomeNotSubmitted
		// The defect, named as itself. Clause 3: an operator reading this must
		// not confuse it with a provider refusal or an agent that had nothing
		// to say — the thirteen-hour misdiagnosis was exactly that confusion.
		slog.Warn("agent_send",
			"component", "agent_send",
			"name", name,
			"status", "not_submitted",
			"outcome", string(outcome),
			"flight", flight.String(),
			"evidence", ev.Detail,
		)
		return agentSendResult{}, fmt.Errorf(
			"message not submitted to %q: the send call succeeded and %s. "+
				"The payload was handed to an idle agent and never became a turn — "+
				"pasted into the composer without a submit, not refused by the provider "+
				"and not an empty reply. The text is in %[1]q's composer: re-sending will "+
				"stack a second copy behind it",
			name, evidenceDetail(ev))
	}
}

// sentStatus names a begun turn on the wire. The three spellings are the
// caller-visible history of how the send got there (fresh, after a rehydrate,
// after an interrupt); ConfirmSendBeganTurn treats all three as begun.
func sentStatus(rehydrated, interrupted bool) string {
	switch {
	case interrupted:
		return "interrupted_sent"
	case rehydrated:
		return "rehydrated_sent"
	default:
		return "sent"
	}
}

// evidenceDetail renders what was observed, never leaving the operator with an
// unexplained verdict.
func evidenceDetail(ev TurnEvidence) string {
	if d := strings.TrimSpace(ev.Detail); d != "" {
		return d
	}
	return "nothing was observed of the agent"
}

// deliverToSender is the pure-ish busy/queue/interrupt path used by
// sendToAgent and hermetic tests with a fake sender.
//
// 🎯T416: a send is no longer reported from its own return value. The call
// returning nil means the keystrokes left; it has never meant the agent read
// them. What the caller is told now comes from ClassifySendOutcome, over
// evidence gathered about THIS PAYLOAD — see turn_flight.go for why three
// answers were not enough and turn_evidence.go for why transcript growth is
// not one of them.
func deliverToSender(s *Server, name, text string, interrupt bool, proc agentSender, rehydrated bool) (agentSendResult, error) {
	return deliverToSenderWith(s, name, text, interrupt, proc, rehydrated, confirmHere)
}

// deliverToSenderWith is deliverToSender with the confirmation owner named.
func deliverToSenderWith(s *Server, name, text string, interrupt bool, proc agentSender, rehydrated bool, confirm sendConfirmation) (agentSendResult, error) {
	if proc == nil || !proc.Alive() {
		return agentSendResult{}, fmt.Errorf("agent %q is not running", name)
	}

	// A turn known to be running is not offered to the provider CLI at all.
	// Handing it over would work — the CLI queues it — but into a store the
	// daemon can neither see nor replay: it merges silently with later sends
	// and dies with the pane at the next rotation or restart (🎯T418). The
	// daemon's own queue drains on terminal stop, one message per turn.
	if s.flightState(name) == FlightInFlight {
		// 🎯T426 clause 3: "in flight" is a claim this process wrote when it
		// last saw a send begin, and it is only worth anything while the sink
		// that would retract it is still attached. Attaching one HERE means it
		// was not: a turn may have begun and ended with nobody watching, and
		// the queue this send is about to join has been held across a boundary
		// the daemon never observed. Say so, to the log and to the sender —
		// reporting a cheerful "queued" over a dark event stream is the exact
		// silence that let six messages stack behind a compacted jevons-po.
		darkStream := s.EnsureAgentEventsWired(name)
		n, qerr := s.enqueueAgentSend(name, text)
		if qerr != nil {
			return agentSendResult{}, fmt.Errorf("NOT DELIVERED and NOT QUEUED: %w", qerr)
		}
		msg := fmt.Sprintf(
			"busy: %q has a turn in flight; message queued (%d pending) for delivery when it ends — "+
				"held by the daemon, not pasted into the agent's composer. "+
				"To cut the current turn short instead: jevons_agent_send with interrupt=true.",
			name, n)
		if darkStream {
			slog.Warn("🎯T426 queued behind an unobserved turn boundary — event stream was dark",
				"agent", name, "queued", n,
				"detail", "in_flight was written before the sink detached; re-attached on this send")
			msg = fmt.Sprintf(
				"queued (%d pending) for %q, BUT ITS IN-FLIGHT RECORD IS NOT TRUSTWORTHY: the daemon had lost "+
					"this agent's event stream (a rotation or restart replaced its process without wiring it), "+
					"so a turn may have ended unobserved and this queue may have been stalled rather than waiting. "+
					"The stream is re-attached now and the queue drains at the next observed turn end. "+
					"If nothing moves, that agent's turn already ended before the re-attach: confirm from ITS "+
					"transcript (terminal assistant message + turn_duration, file not growing) and then use "+
					"jevons_agent_send with interrupt=true.",
				n, name)
		}
		res := agentSendResult{Status: "queued", Message: msg, Queued: n}
		logAgentSendOutcome(name, res, rehydrated, OutcomeQueuedBehindTurn, FlightInFlight, TurnEvidence{})
		return res, nil
	}

	trySend := func() error { return proc.Send(text) }

	// Opened BEFORE the send so "the payload arrived" is measured against a
	// pre-send baseline, never against what an earlier session left on disk.
	flight := s.flightState(name)
	var watch turnWatch
	if confirm == confirmHere {
		watch = s.watchAgentTurnFor(name, text)
	}

	err := trySend()
	if err == nil {
		if confirm == confirmByCaller {
			// The spawn path judges this one; reporting a verdict here would
			// hand it a status its own predicate does not know.
			res := agentSendResult{
				Status:  sentStatus(rehydrated, false),
				Message: fmt.Sprintf("Message sent to %q. You will be notified when it responds.", name),
				Queued:  s.pendingAgentSends(name),
			}
			logAgentSendResult(name, res, rehydrated)
			s.markAgentTurnBegan(name)
			s.noteTurnInFlight(name)
			return res, nil
		}
		ev := watch()
		outcome := ClassifySendOutcome(flight, ev)
		return s.reportSendOutcome(name, outcome, flight, ev, rehydrated, false)
	}

	if !isPromptInFlight(err) {
		// 🎯T237: structured class + owner-visible copy (not bare Internal error).
		class, ownerMsg := agenterr.ClassifyAndFormat(err)
		if !class.IsFailure() {
			ownerMsg = err.Error()
		}
		slog.Warn("agent_send",
			"component", "agent_send",
			"name", name,
			"status", "failed",
			"failure_class", class.String(),
			"transient", class.IsTransient(),
			"err", err.Error(),
		)
		return agentSendResult{}, fmt.Errorf("send failed: %s", ownerMsg)
	}

	// Busy path (🎯T111.1): interrupt then send, or queue for after turn.
	if interrupt {
		if ierr := proc.Interrupt(); ierr != nil {
			// Still queue so the nudge is not lost; surface both facts.
			n, qerr := s.enqueueAgentSend(name, text)
			if qerr != nil {
				return agentSendResult{}, fmt.Errorf("NOT DELIVERED and NOT QUEUED: %w", qerr)
			}
			res := agentSendResult{
				Status: "interrupted_queued",
				Message: fmt.Sprintf(
					"busy: prompt already in flight; interrupt failed (%v); message queued (%d pending) for delivery after the turn. "+
						"Stuck recovery without kill: re-send with interrupt=true after a moment, or jevons_agent_stop then jevons_agent_start (resume session).",
					ierr, n),
				Queued: n,
			}
			logAgentSendResult(name, res, rehydrated)
			return res, nil
		}
		// Brief yield so ACP can clear promptID after session/cancel.
		time.Sleep(50 * time.Millisecond)
		// A successful interrupt ends the turn that was running, so the agent
		// is known idle for this send and the strict answer applies: after
		// cutting a turn short, "it went into the composer and stayed there"
		// is precisely the outcome the caller must not hear as success.
		watch2 := s.watchAgentTurnFor(name, text)
		if err2 := trySend(); err2 == nil {
			ev := watch2()
			outcome := ClassifySendOutcome(FlightIdle, ev)
			return s.reportSendOutcome(name, outcome, FlightIdle, ev, rehydrated, true)
		} else if isPromptInFlight(err2) {
			n, qerr := s.enqueueAgentSend(name, text)
			if qerr != nil {
				return agentSendResult{}, fmt.Errorf("NOT DELIVERED and NOT QUEUED: %w", qerr)
			}
			res := agentSendResult{
				Status: "interrupted_queued",
				Message: fmt.Sprintf(
					"busy: interrupted %q but prompt still in flight; message queued (%d pending). "+
						"Will deliver when the turn ends. Recovery without kill+remint: wait, or stop+start (resume).",
					name, n),
				Queued: n,
			}
			logAgentSendResult(name, res, rehydrated)
			return res, nil
		} else {
			class, ownerMsg := agenterr.ClassifyAndFormat(err2)
			if !class.IsFailure() {
				ownerMsg = err2.Error()
			}
			slog.Warn("agent_send",
				"component", "agent_send",
				"name", name,
				"status", "failed_after_interrupt",
				"failure_class", class.String(),
				"transient", class.IsTransient(),
				"err", err2.Error(),
			)
			return agentSendResult{}, fmt.Errorf("send after interrupt failed: %s", ownerMsg)
		}
	}

	n, qerr := s.enqueueAgentSend(name, text)
	if qerr != nil {
		return agentSendResult{}, fmt.Errorf("NOT DELIVERED and NOT QUEUED: %w", qerr)
	}
	res := agentSendResult{
		Status: "queued",
		Message: fmt.Sprintf(
			"busy: prompt already in flight on %q; message queued (%d pending) for delivery when the current turn ends. "+
				"Not a dead-end — no silent drop. To interrupt a stuck turn: jevons_agent_send with interrupt=true "+
				"(or stop+start to resume the same session without kill/remint).",
			name, n),
		Queued: n,
	}
	logAgentSendResult(name, res, rehydrated)
	return res, nil
}

// liveSender finds the process already running for name, without rehydrating.
// The drain fires on a turn boundary, so a missing process means the agent went
// away mid-queue: the message waits for the next launch rather than resurrecting
// one. The deliver seam is consulted first, which is what lets clause 4's
// no-duplicate rule be exercised without a provider process (🎯T416).
func (s *Server) liveSender(name string) (agentSender, bool) {
	s.mu.Lock()
	resolve := s.resolveSender
	s.mu.Unlock()
	if resolve != nil {
		proc, _, err := resolve(name)
		if err != nil || proc == nil || !proc.Alive() {
			return nil, false
		}
		return proc, true
	}
	if s.registry == nil {
		return nil, false
	}
	proc := s.registry.Get(name)
	if proc == nil || !proc.Alive() {
		return nil, false
	}
	return proc, true
}

// drainAgentSendQueue delivers the next queued message after a terminal
// stop. Called from the agent event sink (🎯T111.1).
func (s *Server) drainAgentSendQueue(name string) {
	entry, ok, err := s.sendQueue().PopFront(name)
	if err != nil || !ok {
		return
	}
	text := entry.Text
	proc, live := s.liveSender(name)
	if !live {
		// Put back at the head, keeping the acceptance age (🎯T418).
		if perr := s.sendQueue().PushFront(name, entry); perr != nil {
			slog.Error("agent send queue: process not alive AND the message could not be returned",
				"component", "agent_send", "name", name, "err", perr)
		} else {
			slog.Warn("agent send queue: process not alive; re-queued", "name", name)
		}
		return
	}
	// The drain runs on a turn boundary, so the agent is idle by construction
	// and the strict answer applies — this is the one moment the daemon is
	// certain what the agent was doing.
	watch := s.watchAgentTurnFor(name, text)
	if err := proc.Send(text); err != nil {
		if isPromptInFlight(err) {
			// Still busy (nested tool pause edge): put at front by re-queue.
			_ = s.sendQueue().PushFront(name, sendq.Entry{Text: text, EnqueuedAt: time.Now()})
			return
		}
		class := agenterr.Classify(err)
		slog.Warn("agent send queue: drain send failed",
			"name", name,
			"err", err,
			"failure_class", class.String(),
			"transient", class.IsTransient(),
		)
		return
	}
	ev := watch()
	if ClassifySendOutcome(FlightIdle, ev) != OutcomeBegun {
		// 🎯T416 clause 4: a drained message that stuck is NOT re-queued. It is
		// sitting in the agent's composer, and re-queueing would deliver a
		// second copy the moment anything submits the first — the silent merge
		// this target exists to end. The backlog becomes visible instead:
		// pending count, the payload's fate, and a fleet-health notice, so an
		// undelivered queue is something an operator is told about rather than
		// something they infer from an agent that never answers.
		remaining := s.pendingAgentSends(name)
		slog.Error("agent send queue: drained message not submitted",
			"component", "agent_send",
			"name", name,
			"status", "not_submitted",
			"evidence", ev.Detail,
			"remaining", remaining,
			"bytes", len(text),
		)
		s.notifyFleetHealth(fmt.Sprintf(
			"Undelivered backlog on %q: a queued message (%d bytes) was pasted after its turn ended and never became a turn — %s. "+
				"It is in that agent's composer, not lost, and %d more are still queued behind it. "+
				"It has NOT been re-queued: that would deliver a duplicate once anything submits the first copy.",
			name, len(text), evidenceDetail(ev), remaining))
		return
	}
	s.markAgentTurnBegan(name)
	s.noteTurnInFlight(name)
	slog.Info("agent send queue: drained one message", "name", name, "remaining", s.pendingAgentSends(name))
}

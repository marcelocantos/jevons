// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/sendq"
	"github.com/marcelocantos/jevons/internal/upgrade"
)

// undeliverableQueueError is what a sender is told when the daemon could not
// write the message to its own queue (🎯T418). It is an error rather than a
// "queued" status on purpose: the whole value of holding a message is that
// something holds it, and a queue that failed to accept it is holding nothing.
func undeliverableQueueError(name string, err error) error {
	return fmt.Errorf(
		"%q has a turn in flight and the daemon could not hold this message for it (%w). "+
			"NOT DELIVERED and NOT QUEUED — re-send it when that agent is idle, or with interrupt=true",
		name, err)
}

// agentSendResult is the outcome of sendToAgent (🎯T111.1).
type agentSendResult struct {
	// Status: sent | queued | steered | interrupted_sent | interrupted_queued | rehydrated_sent
	Status  string
	Message string
	Queued  int // pending after this call (including this message if queued)
	// Mode is the owner's delivery intent and Mechanism what actually ran
	// (🎯T657). Spellings: internal/delivery.
	Mode      delivery.Mode
	Mechanism string
	// InterruptAfter is when an escalating send (🎯T899) interrupts the
	// busy turn if the agent has not taken the message; 0 = no rung.
	InterruptAfter time.Duration
	// ReceiverReceipt is positive evidence for this exact payload from the
	// receiver's user-message or queue-operation records. A status of sent
	// alone can be inferred from an unrelated live session event (🎯T416).
	ReceiverReceipt bool
}

// agentSender is the process surface sendToAgent needs (testable).
type agentSender interface {
	Send(text string) error
	Interrupt() error
}

// readoptDeliver runs op on proc and, when proc is the real broker-held agent
// and the broker answers not_owner, re-adopts the seat and retries once on the
// fresh handle (🎯T796). Test doubles are delivered to as they are.
func readoptDeliver(name string, proc agentSender, op func(agentSender) error) error {
	real, ok := proc.(*claudia.Agent)
	if !ok {
		return op(proc)
	}
	return upgrade.WithReadopt(context.Background(), name, real,
		func(a *claudia.Agent) error { return op(a) })
}

// isPromptInFlight reports a concurrent prompt/turn (busy) so senders
// queue or interrupt instead of hard-failing (🎯T111.1, 🎯T214 J6).
// Provider-agnostic: not Grok ACP string only — see agenterr.IsPromptBusy.
func isPromptInFlight(err error) bool {
	return agenterr.IsPromptBusy(err)
}

// enqueueAgentSend appends text for delivery after the current turn and
// returns the total pending for name.
//
// 🎯T418: the error is returned rather than logged, and every caller reports it
// instead of answering "queued". A daemon that could not write the queue is not
// holding the message, and "queued (N pending) … held by the daemon" is then a
// claim about a store that does not contain it.
func (s *Server) enqueueAgentSend(name, text string) (int, error) {
	if IsIdleNudgeText(text) {
		// 🎯T821: a held idle nudge is stale the moment the next one is
		// composed; keep one pending nudge per seat, not a growing stack.
		_, depth, replaced, err := s.sendQueue().AppendSuperseding(name, text, time.Now(),
			func(e sendq.Entry) bool { return IsIdleNudgeText(e.Text) })
		if replaced > 0 {
			slog.Info("idle nudge superseded held nudge",
				"component", "agent_send", "name", name, "replaced", replaced)
		}
		return depth, err
	}
	_, depth, err := s.sendQueue().Append(name, text, time.Now())
	return depth, err
}

// dequeueAgentSend pops the oldest pending message, or "" if empty. An
// unreadable queue is reported as empty AND logged: the drain runs on an event,
// with nobody to return an error to, so the alternative to logging is silence.
func (s *Server) dequeueAgentSend(name string) sendq.Entry {
	e, ok, err := s.sendQueue().PopFront(name)
	if err != nil {
		slog.Error("agent send queue: unreadable; nothing drained",
			"component", "agent_send", "name", name, "err", err)
		return sendq.Entry{}
	}
	if !ok {
		return sendq.Entry{}
	}
	return e
}

func (s *Server) pendingAgentSends(name string) int {
	st, _ := s.Seats().Get(name)
	return st.QueueDepth
}

// AgentDeliverResult is the public outcome of DeliverAgentMessage (🎯T275).
// Status matches MCP jevons_agent_send: sent | queued | interrupted_sent |
// interrupted_queued | rehydrated_sent.
type AgentDeliverResult struct {
	Status  string
	Message string
	Queued  int
	// Mode and Mechanism: the owner's intent and what ran (🎯T657).
	Mode      string
	Mechanism string
	// InterruptAfterMS: see agentSendResult.InterruptAfter (🎯T899).
	InterruptAfterMS int64
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
		s.notifyDeadAgents(s.sweepDeadAccounted())
	}

	proc := s.registry.Get(name)
	if proc != nil && s.seatState(name).Alive == seatstate.Unknown {
		return nil, false, fmt.Errorf("agent %q liveness is unknown; awaiting observation", name)
	}
	if proc != nil && (s.seatState(name).Alive == seatstate.Yes) {
		return proc, false, nil
	}
	if s.registry.Def(name) == nil {
		// 🎯T401: prefer the reaped-with-reason signal over a bare not-running
		// when the intent store still remembers the auto-deregistration.
		// deliverByName holds the payload before this arm runs; other callers
		// still get the informative decline rather than a blank not-running.
		if rec, ok := LookupReapedRecord(s.fleetIntent(), name); ok {
			return nil, false, fmt.Errorf(
				"agent %q was auto-deregistered (reaped-with-reason): %s — recover with jevons_agent_start name=%s (or jevons_fleet_intent state=working then start); use jevons_agent_send to hold a message in sendq",
				name, rec.Describe(), name)
		}
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

	// 🎯T409: lost-session recovery belongs on this path. It is what a
	// parent report and the impatience ladder call, and they retry on a
	// timer. registry.Launch latches a Cursor session/load refusal
	// (RequireResume after a bounce, even when store.db was never written)
	// and every later call returns that same error. SessionLost does not
	// see a Cursor row, so the rotate-before-launch arm never engages.
	// LaunchRecovering rotates once after that refusal and launches the
	// fresh id; a store a leftover still holds is not rotated (🎯T541.1).
	p2, err := fleet.LaunchRecovering(s.registry, name)
	if err != nil {
		return nil, false, fmt.Errorf("agent %q is not running and rehydrate failed: %v", name, err)
	}
	s.wireAgentEvents(name, p2)
	s.noteSeatMinted(name) // 🎯T597: rehydrate is a (re-)mint for activity baselines
	slog.Info("agent send rehydrated dead/stopped process", "name", name)
	return p2, true, nil
}

// logAgentSendResult emits structured slog for busy/queue/interrupt outcomes
// (🎯T120.2). Shared field schema: component, name, status, queued, rehydrated.
// 🎯T657: the same line carries mode and mechanism, so a steer that fell back
// to a queue is legible as one without a second log line.
func logAgentSendResult(name string, res agentSendResult, rehydrated bool) {
	slog.Info("agent_send",
		"component", "agent_send",
		"name", name,
		"status", res.Status,
		"queued", res.Queued,
		"rehydrated", rehydrated,
		"mode", string(res.Mode),
		"mechanism", res.Mechanism,
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
		"mode", string(res.Mode),
		"mechanism", res.Mechanism,
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
//
// transportErr is the send call's own error where there was one, and it is
// evidence about the CALL, never about the receiver (🎯T429). It is carried
// through so the operator sees both accounts — what the harness claimed and what
// the receiver's records showed — because the whole defect is the first being
// reported as though it were the second.
func (s *Server) reportSendOutcome(name, payload string, outcome SendOutcome, flight TurnFlight, ev TurnEvidence, rehydrated, interrupted bool, transportErr error, mm sendMech) (agentSendResult, error) {
	claim := describeTransportClaim(transportErr)
	switch outcome {
	case OutcomeBegun:
		msg := fmt.Sprintf("Message sent to %q. You will be notified when it responds.", name)
		switch {
		case interrupted:
			msg = fmt.Sprintf("Interrupted in-flight turn on %q and sent the new message.", name)
		case rehydrated:
			msg += s.rehydratedAfter(name) // 🎯T662: the recorded reason, not a bare "dead/stopped"
		}
		if claim != "" {
			// The false negative, caught in the act and counted. This line is
			// the only standing measurement of how often the harness condemns a
			// delivery that happened, and it is what tells the claudia side
			// (clause 3) whether its submit loop is getting better.
			slog.Warn("🎯T429 transport claimed failure for a payload the receiver holds",
				"component", "agent_send",
				"name", name,
				"transport_claim", claim,
				"evidence", ev.Detail,
			)
			msg += fmt.Sprintf(
				" (the send transport reported %q — a claim about its own pane capture, "+
					"contradicted by %s)", claim, evidenceDetail(ev))
		}
		msg += describeMode(mm)
		res := agentSendResult{Status: sentStatus(rehydrated, interrupted), Message: msg, Queued: s.pendingAgentSends(name), Mode: mm.Mode, Mechanism: mm.Mechanism, ReceiverReceipt: ev.PayloadSeen || ev.PayloadEnteredTurn}
		logAgentSendOutcome(name, res, rehydrated, outcome, flight, ev)
		// 🎯T305: never_briefed → running. Now earned from the payload
		// arriving rather than from the send call returning.
		s.markAgentTurnBegan(name)
		s.noteTurnInFlight(name)
		// 🎯T417: durable delivery evidence survives later compaction.
		s.recordDeliveryEvidence(name, payload, ev)
		// 🎯T906: a begun turn is evidence the SEAT accepted the payload, not
		// that the provider answered it — the seat's own next turn can still
		// fail on the same wall (T885/T905 401 shape). Only authored assistant
		// text (chat.go DeliverOverseerEvent) or a real reply
		// (replyFailure/jwork/event_push) is evidence the provider is back.
		return res, nil

	case OutcomeUnconfirmed:
		// The one answer that exists because state can be lost. Not success:
		// the caller must not read this as delivered. Not a defect either:
		// after a restart the daemon has no record of a turn it never saw
		// start, and a healthy send to a busy agent looks exactly like this.
		msg := fmt.Sprintf(
			"Message handed to %q but NOT confirmed as a turn: %s. "+
				"This daemon has no record of whether %[1]q already had a turn running "+
				"(that record does not survive a restart), so it cannot tell a message waiting "+
				"behind a live turn from one left sitting in the composer.",
			name, evidenceDetail(ev))
		if claim != "" {
			msg += fmt.Sprintf(
				" The send transport reported %q — that is its reading of a terminal frame, "+
					"not an observation of %[2]q, and the same reading has condemned payloads "+
					"the receiver was already working from.", claim, name)
		}
		// 🎯T429 clause 4. What used to sit here was retry advice, and a retry
		// on this verdict is precisely the wrong move: nine composer copies were
		// stacked that way on 2026-08-10, each re-send behind a message that had
		// in fact landed. An unknown is resolved by looking, not by sending
		// again, so the instrument that can decide it is named instead.
		msg += fmt.Sprintf(
			" DO NOT re-send, and do not retry with interrupt=true, on this verdict — "+
				"a re-send on a false negative stacks a second copy in %[1]q's composer. "+
				"Decide it by reading %[1]q's own session records (jevons_transcript_read): "+
				"a user message carrying the payload, or a queue-operation "+
				"enqueue/dequeue/remove/popAll or queued_command attachment carrying it, "+
				"means it was delivered. Absence at user-message level alone does not mean lost.",
			name)
		// 🎯T664: remember the undecided delivery so stop / kill refuse to act
		// on it, and say so here, where the verdict is read.
		s.noteUnconfirmedSend(name, payload)
		msg += fmt.Sprintf(" Do not stop or kill %q on this verdict either: jevons_agent_stop and jevons_agent_kill refuse it until a turn boundary or a transcript read decides it (🎯T664).", name)
		res := agentSendResult{
			Status:    "delivered_unconfirmed",
			Message:   msg,
			Queued:    s.pendingAgentSends(name),
			Mode:      mm.Mode,
			Mechanism: mm.Mechanism,
		}
		logAgentSendOutcome(name, res, rehydrated, outcome, flight, ev)
		return res, nil

	case OutcomeQueuedBehindTurn:
		res := agentSendResult{
			Status: "queued",
			Message: fmt.Sprintf(
				"busy: %q had a turn in flight; message queued (%d pending) for delivery when it ends.",
				name, s.pendingAgentSends(name)),
			Queued:          s.pendingAgentSends(name),
			Mode:            mm.Mode,
			Mechanism:       delivery.MechanismClientQueue,
			ReceiverReceipt: ev.PayloadQueued,
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
			"transport_claim", claim,
		)
		// 🎯T429: this verdict is still reachable, and reaching it is the point.
		// It is earned from a POSITIVE observation of the receiver — its session
		// file was never created, or it was known idle and its transcript never
		// took the payload — and not from the send call having returned an error.
		// A fix that answered "unknown" everywhere would be the same defect
		// pointing the other way.
		how := "the send call succeeded"
		if claim != "" {
			how = fmt.Sprintf("the send transport reported %q, which agrees with what was observed", claim)
		}
		return agentSendResult{}, fmt.Errorf(
			"message not submitted to %q: %s and %s. "+
				"The payload was handed to an idle agent and never became a turn — "+
				"pasted into the composer without a submit, not refused by the provider "+
				"and not an empty reply. The text is in %[1]q's composer: re-sending will "+
				"stack a second copy behind it",
			name, how, evidenceDetail(ev))
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
// The bool is the deprecated interrupt alias (🎯T657); named modes use
// deliverToSenderMode directly.
func deliverToSenderWith(s *Server, name, text string, interrupt bool, proc agentSender, rehydrated bool, confirm sendConfirmation) (agentSendResult, error) {
	mode := delivery.ModeSubmit
	if interrupt {
		mode = delivery.ModeInterrupt
	}
	return deliverToSenderMode(s, name, text, mode, proc, rehydrated, confirm)
}

func (s *Server) lockAgentSend(name string) func() {
	s.mu.Lock()
	if s.agentSendLocks == nil {
		s.agentSendLocks = make(map[string]*sync.Mutex)
	}
	lock := s.agentSendLocks[name]
	if lock == nil {
		lock = &sync.Mutex{}
		s.agentSendLocks[name] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// The provider may know a turn is open before Jevons has observed a turn
// event. OMP marks that phase before writing the prompt to its sidecar.
// deliverToSenderMode is the mode-carrying send path (🎯T657).
//
//   - submit: send when idle; queue behind a known or discovered open turn
//     (mechanism client_queue).
//   - steer: through the process's SendMode when it has one — folded into the
//     open turn (status steered, mechanism as claudia reports it) or a plain
//     submit when the seat was idle. Without SendMode, or when claudia says
//     steer is unsupported, the text is queued and the mechanism says
//     queue_until_idle — never "steered".
//   - interrupt: the 🎯T424 hatch — cancel, then send (mechanism
//     session_cancel+prompt when the daemon ran the cancel itself).
//   - queue: hold for the next turn boundary when a turn is open; an idle seat
//     has no boundary coming, so the text is submitted (mechanism submit).
func deliverToSenderMode(s *Server, name, text string, mode delivery.Mode, proc agentSender, rehydrated bool, confirm sendConfirmation) (agentSendResult, error) {
	s.observeQueue(name)
	// A sidecar socket write returns before the provider accepts or rejects
	// the prompt. Serialize sends to one seat through the witness verdict so
	// simultaneous callers cannot both mistake that interval for idle.
	unlock := s.lockAgentSend(name)
	defer unlock()
	if proc == nil || !(s.seatState(name).Alive == seatstate.Yes) {
		return agentSendResult{}, fmt.Errorf("agent %q is not running", name)
	}
	if mode == "" {
		mode = delivery.ModeSubmit
	}
	interrupt := mode.Interrupts()
	mm := sendMech{Mode: mode, Mechanism: delivery.MechanismSubmit}

	// The seam through which a steer reaches the seat. Nil on the pinned
	// claudia (🎯T448): the mode then degrades to submit-or-queue, said once.
	var seam sendModeFunc
	if mode == delivery.ModeSteer {
		seam = sendModeSeam(proc)
		if seam == nil {
			logSendModeSeamMissing(name)
		}
	}

	// A turn known to be running is not offered to the provider CLI at all.
	// Handing it over would work — the CLI queues it — but into a store the
	// daemon can neither see nor replay: it merges silently with later sends
	// and dies with the pane at the next rotation or restart (🎯T418). The
	// daemon's own queue drains on terminal stop, one message per turn.
	//
	// 🎯T424: interrupt=true never takes this arm. The reading can be
	// stale in both directions; the hatch must act on the process, not on
	// the flag it exists to escape.
	//
	// 🎯T657: a steer with a seam does not take it either — folding into the
	// open turn is the point, and the seam decides on the process's own
	// phase reading rather than this one.
	if !interrupt && seam == nil && (s.flightState(name) == FlightInFlight) {
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
			return agentSendResult{}, undeliverableQueueError(name, qerr)
		}
		mm.Mechanism = delivery.MechanismClientQueue
		if mode == delivery.ModeSteer {
			mm.Mechanism = delivery.MechanismQueueUntilIdle
		}
		msg := fmt.Sprintf(
			"busy: %q has a turn in flight; message queued (%d pending) for delivery when it ends — "+
				"held by the daemon, not pasted into the agent's composer. "+
				"To cut the current turn short instead: jevons_agent_send with mode=interrupt.",
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
					"jevons_agent_send with mode=interrupt.",
				n, name)
		}
		res := agentSendResult{Status: "queued", Message: msg + describeMode(mm), Queued: n, Mode: mode, Mechanism: mm.Mechanism}
		logAgentSendOutcome(name, res, rehydrated, OutcomeQueuedBehindTurn, FlightInFlight, TurnEvidence{})
		return res, nil
	}

	// 🎯T711: mode=interrupt cuts the turn on the PROCESS before the text is
	// offered to it, rather than after the offer bounces back.
	//
	// The old order made the hatch conditional on the seat's process refusing
	// a second prompt. That is true of a backend which answers "prompt already
	// in flight" (Grok ACP) and false of every backend that accepts the text
	// into its own client queue and returns nil — the ordinary Claude-shaped
	// seat, and therefore every PO, which is mid-turn nearly all the time. On
	// those seats interrupt=true never reached proc.Interrupt() at all: the
	// text went into the CLI's queue, the classifier read it as waiting behind
	// a live turn, and the caller was told `queued` (mechanism client_queue)
	// while the turn it had asked to cut ran on. An owner direct to a working
	// PO is precisely the message that must not wait its turn — the 2026-09-20
	// ge-po specimen, where a doctrine direct with interrupt=true came back
	// queued twice.
	//
	// 🎯T424 already required the hatch to act on the process rather than on a
	// flag or a flight reading. It said so one step too late: in the arm only
	// a bouncing backend can reach.
	preFlight := s.flightState(name)
	interrupted := false
	if interrupt {
		if ierr := readoptDeliver(name, proc, func(a agentSender) error { return a.Interrupt() }); ierr != nil {
			// A turn this daemon watched begin is a turn the caller asked to
			// cut. Failing that is an error, never a graceful enqueue (🎯T424).
			if preFlight == FlightInFlight {
				return agentSendResult{}, fmt.Errorf(
					"interrupt failed for %q (%v) — message was not queued (🎯T424). "+
						"The turn could not be cut; jevons_agent_stop then jevons_agent_start resumes the session.",
					name, ierr)
			}
			// Nothing was known to be running, so a refused cancel may mean
			// only that there was no turn to cancel. Offer the text, and leave
			// the post-send busy arm below as the backstop for a seat that
			// turns out to be working after all.
			slog.Info("🎯T711 pre-send interrupt refused on a seat with no observed turn; offering the text anyway",
				"component", "agent_send", "name", name,
				"flight", preFlight.String(), "err", ierr.Error())
		} else {
			interrupted = true
			s.cancelMCPFlights(name)
			mm.Mechanism = delivery.MechanismSessionCancelPrompt
			// Brief yield so ACP can clear promptID after session/cancel.
			time.Sleep(50 * time.Millisecond)
		}
	}

	// trySend is the one place text reaches the process. Through the seam it
	// also reports what ran and the phase the process saw itself in.
	trySend := func() (sendModeOutcome, error) {
		if seam != nil {
			return seam(text, mode)
		}
		return sendModeOutcome{Mechanism: delivery.MechanismSubmit}, readoptDeliver(name, proc, func(a agentSender) error { return a.Send(text) })
	}

	// Opened BEFORE the send so "the payload arrived" is measured against a
	// pre-send baseline, never against what an earlier session left on disk.
	flight := s.flightState(name)
	if interrupted {
		// 🎯T711: the cut ended whatever was running, so this payload is being
		// handed to a seat known idle — and the strict verdict applies. After
		// cutting a turn short, "it went into the composer and stayed there"
		// is exactly the outcome the caller must not hear as success.
		flight = FlightIdle
	}
	var watch turnWatch
	if confirm == confirmHere {
		watch = s.watchAgentTurnFor(name, text)
	}

	out, err := trySend()
	if err == nil {
		// 🎯T711: a cut already named the mechanism (session_cancel+prompt),
		// and the plain Send that follows it reports the generic "submit".
		// Letting that overwrite would erase the only part of the answer that
		// says the turn was cut rather than joined.
		if out.Mechanism != "" && !interrupted {
			mm.Mechanism = out.Mechanism
		}
		if seam != nil && out.PhaseBefore == delivery.PhaseInTurn {
			// Folded into the open turn: there is no new turn to watch begin,
			// and the queue is untouched. The mechanism is claudia's own word
			// for what it did (ACP supersede, Codex turn/steer, …).
			res := agentSendResult{
				Status:    statusSteered,
				Message:   fmt.Sprintf("Steered the in-flight turn on %q with the new text.", name) + describeMode(mm),
				Queued:    s.pendingAgentSends(name),
				Mode:      mode,
				Mechanism: mm.Mechanism,
			}
			logAgentSendResult(name, res, rehydrated)
			s.noteTurnInFlight(name)
			return res, nil
		}
		if confirm == confirmByCaller {
			// The spawn path judges this one; reporting a verdict here would
			// hand it a status its own predicate does not know.
			res := agentSendResult{
				Status:    sentStatus(rehydrated, interrupted),
				Message:   fmt.Sprintf("Message sent to %q. You will be notified when it responds.", name) + describeMode(mm),
				Queued:    s.pendingAgentSends(name),
				Mode:      mode,
				Mechanism: mm.Mechanism,
			}
			logAgentSendResult(name, res, rehydrated)
			s.markAgentTurnBegan(name)
			s.noteTurnInFlight(name)
			return res, nil
		}
		ev := watch()
		outcome := s.classifySend(name, text, flight, ev)
		return s.reportSendOutcome(name, text, outcome, flight, ev, rehydrated, interrupted, nil, mm)
	}

	// 🎯T657: claudia answered that this handle cannot steer. Not busy, not a
	// transport failure — the honest answer is to hold the text.
	steerRefused := mode == delivery.ModeSteer && isSteerUnsupported(err)

	// 🎯T745: a no_composer stall on a seat whose transcript just moved is a
	// pane busy rendering, not a CLI that never started. Hold the text as for
	// any busy turn instead of failing the send (which re-pressures on a loop).
	busyPane := !isPromptInFlight(err) && s.sendStallIsBusyPane(name, err)

	if !isPromptInFlight(err) && !steerRefused && !busyPane {
		// 🎯T429: ask the error the narrow question first — does it DISPROVE
		// delivery? A transport that could not verify a submission has not
		// observed the receiver at all, and returning its claim as a failure is
		// how a payload the receiver was already working from came back to the
		// caller as `turn not submitted`. Where the payload may have landed, the
		// same instrument the success path uses decides: the watch is already
		// open, taken from a pre-send baseline, and it reads the RECEIVER.
		if claim := ClassifySendError(err); !claim.DisprovesDelivery() && confirm == confirmHere {
			ev := watch()
			outcome := s.classifySend(name, text, flight, ev)
			return s.reportSendOutcome(name, text, outcome, flight, ev, rehydrated, interrupted, err, mm)
		}
		// 🎯T661: a broker refusal of the line is about the receiver's session,
		// not this payload — say which records, and how to recover.
		if IsBrokerLineLimitError(err) && s.registry != nil {
			if d := s.registry.Def(name); d != nil {
				if lines := s.seatOversized(*d, DefaultSessionRoots()); len(lines) > 0 {
					advice := OversizedSendAdvice(name, len(text), lines, err)
					slog.Warn("agent_send", "component", "agent_send", "name", name,
						"status", "failed", "mode", string(mode), "failure_class", "oversized_session",
						"oversized_records", len(lines), "err", err.Error())
					return agentSendResult{}, errors.New(advice)
				}
			}
		}
		// 🎯T237: structured class + owner-visible copy (not bare Internal error).
		class, ownerMsg := agenterr.ClassifyAndFormat(err)
		if !class.IsFailure() {
			ownerMsg = err.Error()
		}
		slog.Warn("agent_send",
			"component", "agent_send",
			"name", name,
			"status", "failed",
			"mode", string(mode),
			"failure_class", class.String(),
			"transient", class.IsTransient(),
			"err", err.Error(),
		)
		s.ObserveProviderFailure(class, err.Error())
		return agentSendResult{}, fmt.Errorf("send failed: %s", ownerMsg)
	}

	// Busy path (🎯T111.1): interrupt then send, or queue for after turn.
	if interrupt {
		ierr := proc.Interrupt()
		s.cancelMCPFlights(name)
		if ierr != nil {
			// 🎯T424: interrupt never silently becomes a queue. The
			// caller asked to cut the turn; failing that is an error,
			// not a graceful enqueue. Do not advise interrupt=true —
			// that is what they just did.
			return agentSendResult{}, fmt.Errorf(
				"interrupt failed for %q (%v) — message was not queued (🎯T424). "+
					"The turn could not be cut; jevons_agent_stop then jevons_agent_start resumes the session.",
				name, ierr)
		}
		mm.Mechanism = delivery.MechanismSessionCancelPrompt
		// Brief yield so ACP can clear promptID after session/cancel.
		time.Sleep(50 * time.Millisecond)
		// A successful interrupt ends the turn that was running, so the agent
		// is known idle for this send and the strict answer applies: after
		// cutting a turn short, "it went into the composer and stayed there"
		// is precisely the outcome the caller must not hear as success.
		watch2 := s.watchAgentTurnFor(name, text)
		if err2 := readoptDeliver(name, proc, func(a agentSender) error { return a.Send(text) }); err2 == nil {
			ev := watch2()
			outcome := s.classifySend(name, text, FlightIdle, ev)
			return s.reportSendOutcome(name, text, outcome, FlightIdle, ev, rehydrated, true, nil, mm)
		} else if !isPromptInFlight(err2) && !ClassifySendError(err2).DisprovesDelivery() {
			// 🎯T429, on the interrupt arm: the same non-observation, and the
			// same rule. An interrupt that succeeded leaves the agent known
			// idle, so the strict verdict is available here — but it has to be
			// earned from the receiver, not inherited from the transport.
			ev := watch2()
			outcome := s.classifySend(name, text, FlightIdle, ev)
			return s.reportSendOutcome(name, text, outcome, FlightIdle, ev, rehydrated, true, err2, mm)
		} else if isPromptInFlight(err2) {
			// 🎯T424: still busy after a successful Interrupt is a
			// typed failure, not a queue increment. The 2026-08-10
			// six-deep backlog was this arm adding to a queue that
			// had stopped draining.
			return agentSendResult{}, fmt.Errorf(
				"interrupt of %q did not cut the turn (prompt still in flight) — message was not queued (🎯T424). "+
					"The hatch could not open; jevons_agent_stop then jevons_agent_start resumes the session.",
				name)
		} else {
			class, ownerMsg := agenterr.ClassifyAndFormat(err2)
			if !class.IsFailure() {
				ownerMsg = err2.Error()
			}
			slog.Warn("agent_send",
				"component", "agent_send",
				"name", name,
				"status", "failed_after_interrupt",
				"mode", string(mode),
				"failure_class", class.String(),
				"transient", class.IsTransient(),
				"err", err2.Error(),
			)
			s.ObserveProviderFailure(class, err2.Error())
			return agentSendResult{}, fmt.Errorf("send after interrupt failed: %s", ownerMsg)
		}
	}

	n, qerr := s.enqueueAgentSend(name, text)
	if qerr != nil {
		return agentSendResult{}, undeliverableQueueError(name, qerr)
	}
	mm.Mechanism = delivery.MechanismClientQueue
	why := "prompt already in flight"
	if busyPane {
		why = "pane is busy rendering (no idle composer, transcript moved recently — not a startup stall, 🎯T745)"
	}
	if mode == delivery.ModeSteer {
		mm.Mechanism = delivery.MechanismQueueUntilIdle
		why = "steer asked for but this seat cannot fold text into its open turn"
		if steerRefused {
			why = "steer asked for but claudia reports this handle cannot steer"
		}
	}
	res := agentSendResult{
		Status: "queued",
		Message: fmt.Sprintf(
			"busy: %s on %q; message queued (%d pending) for delivery when the current turn ends. "+
				"Not a dead-end — no silent drop. To interrupt a stuck turn: jevons_agent_send with mode=interrupt "+
				"(or stop+start to resume the same session without kill/remint).",
			why, name, n) + describeMode(mm),
		Queued:    n,
		Mode:      mode,
		Mechanism: mm.Mechanism,
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
		if err != nil || proc == nil || !(s.seatState(name).Alive == seatstate.Yes) {
			return nil, false
		}
		return proc, true
	}
	if s.registry == nil {
		return nil, false
	}
	proc := s.registry.Get(name)
	if proc == nil || !(s.seatState(name).Alive == seatstate.Yes) {
		return nil, false
	}
	return proc, true
}

// drainAgentSendQueue delivers the next queued message after a terminal
// stop. Called from the agent event sink (🎯T111.1).
func (s *Server) drainAgentSendQueue(name string) {
	for s.drainAgentSendQueueOnce(name) {
		// A terminal arrived before its message's witness completed. Its first
		// drain saw the held attempt; run that wakeup again after resolution.
	}
}

func (s *Server) drainAgentSendQueueOnce(name string) bool {
	s.confirmCodexReceipts(name)
	q := s.sendQueue()
	// 🎯T774: a pile of pending messages is one digest, not one turn apiece.
	entry, claimed, err := q.ClaimDigest(name)
	if err == nil && !claimed {
		entry, claimed, err = q.ClaimFront(name)
	}
	if err != nil {
		slog.Error("agent send queue: cannot persist delivery attempt", "name", name, "err", err)
		return false
	}
	if !claimed {
		return false // Empty, or an unresolved attempt which must never be replayed.
	}
	resolve := func(outcome sendq.AttemptOutcome, detail string) bool {
		if err := q.Resolve(name, entry, outcome, detail); err != nil {
			slog.Error("agent send queue: cannot record delivery outcome", "name", name,
				"entry_id", entry.ID, "attempt_id", entry.AttemptID, "err", err)
			s.notifyFleetHealth(entry.ID, fmt.Sprintf("Delivery outcome for %q message %s could not be recorded: %v. "+
				"Reconcile the held attempt before sending another copy.", name, entry.ID, err))
			return false
		}
		return true
	}
	proc, live := s.liveSender(name)
	if !live {
		if resolve(sendq.DefinitelyNotSent, "no live process before send") {
			s.noteSendqDeliveryFailure(name, entry, "no live process at drain")
		}
		return false
	}
	// 🎯T731: the queued copy may have been accepted while the author was
	// still registered. Stamp it at flush, which is when the parent actually
	// reads it. Do not suppress here — this drain is the first delivery.
	// A digest already carries its members' text; per-report stamping would
	// misread the "[Agent X responded]" lines quoted inside it.
	text := entry.Text
	if entry.Members == "" {
		text = s.prepareParentReport(name, entry.Text, true).Text
	}
	watch, cancel := s.watchAgentTurnForCancelable(name, entry.Text, turnConfirmWindow())
	defer cancel()
	generation := s.terminalGeneration(name)
	sendErr := readoptDeliver(name, proc, func(a agentSender) error { return a.Send(text) })
	// Busy refusals may still have enqueued the payload in the receiver (🎯T447).
	// Watch before treating the attempt as failed — broker-wrapped errors miss
	// queueSendDefinitelyNotSent's exact-string match and would otherwise land
	// as uncertain/PINNED even when the transcript shows PayloadQueued.
	ev := watch()
	outcome := ClassifySendOutcome(FlightIdle, ev)
	if outcome == OutcomeQueuedBehindTurn {
		detail := evidenceDetail(ev)
		if sendErr != nil {
			detail = describeTransportClaim(sendErr) + "; " + detail
		}
		if resolve(sendq.Confirmed, detail) {
			s.clearSendqPin(name)
			slog.Info("agent send queue: message waiting in receiver queue behind live turn",
				"name", name, "entry_id", entry.ID, "detail", detail)
		}
		return false
	}
	if queueSendDefinitelyNotSent(sendErr) {
		if resolve(sendq.DefinitelyNotSent, sendErr.Error()) && !isPromptInFlight(sendErr) {
			s.noteSendqDeliveryFailure(name, entry, sendErr.Error())
		}
		return false
	}
	// Preserve the existing successful-send verdict in this durability slice.
	// Its generic live-event branch is inherited receipt-quality residue under
	// T623, not a new guarantee of correlated delivery. On an errored send only
	// evidence of this payload may release the obligation; activity alone cannot.
	begun := outcome == OutcomeBegun
	if sendErr != nil && !ev.PayloadSeen && !ev.PayloadEnteredTurn {
		begun = false
	}
	if !begun {
		detail := evidenceDetail(ev)
		if sendErr != nil {
			detail = describeTransportClaim(sendErr) + "; " + detail
		}
		if resolve(sendq.Unverified, detail) {
			s.reportDrainedSendNotBegun(name, entry, ev, time.Now())
		}
		return false
	}
	if !resolve(sendq.Confirmed, evidenceDetail(ev)) {
		return false
	}
	s.markAgentTurnBegan(name)
	ended := s.noteQueuedTurnBegan(name, generation)
	if !ended {
		s.wedges.handedOver(name, entry.ID)
	}
	s.clearSendqPin(name)
	slog.Info("agent send queue: drained one message", "name", name, "entry_id", entry.ID,
		"remaining", s.pendingAgentSends(name))
	return ended
}

// queueSendDefinitelyNotSent reports whether a drain's send provably never
// reached the receiver, so the entry goes back to Pending and is retried on
// the next turn boundary. The bias is deliberate and asymmetric: a false
// "not sent" duplicates a delivery, a false "uncertain" only stalls one
// entry behind a pin the owner can see. So doubt resolves to uncertain.
//
// 🎯T766: this question has been answered two wrong ways in a row, and both
// were failures of the same kind — classifying prose.
//
// It began as an exact-string switch over four provider literals. Every
// error from a broker-hosted seat arrives wrapped as
// "broker protocol: agent_failed: cursor acp: prompt already in flight",
// which equals none of them, so a plain busy refusal was not "definitely not
// sent": the entry resolved Unverified, the queue head became Uncertain, and
// ClaimFront refuses a non-Pending head. One refusal froze a seat's entire
// queue with no automatic exit — jevons-po sat at 87 stranded messages, the
// oldest fourteen hours old, and the only exit anyone used was a reconcile
// that discards the payload.
//
// Replacing it with agenterr.IsPromptBusy, which matches on substring,
// bought the wrapping and sold the guarantee: "provider response quoted:
// grok acp: prompt already in flight" is a report *about* a refusal, not a
// refusal, and substring matching cannot tell those apart. Neither can any
// other textual rule, because both strings are a prefix followed by the same
// phrase. That is 🎯T623's own case, and it caught this.
//
// So this does not match text at all where it matters. It decodes the one
// envelope whose format is defined — broker.ProtocolError.Error(), whose Msg
// is the provider's own error verbatim — and compares that exactly. A quoted
// report is not that envelope, so it stays uncertain; a wrapped refusal is,
// so it retries.
//
// The real seam is claudia exporting a predicate over its typed
// *broker.ProtocolError, which Go's internal/ rule puts out of reach from
// here (🎯T767).
func queueSendDefinitelyNotSent(err error) bool {
	if err == nil {
		return false
	}
	switch providerRefusalText(err) {
	case "claude process not running",
		"grok acp: prompt already in flight", "cursor acp: prompt already in flight",
		"codex app-server: turn already in flight":
		return true
	default:
		return false
	}
}

// brokerAgentFailedPrefix is how claudia renders a provider failure the
// daemon relayed: broker.ProtocolError.Error() with Code CodeAgentFailed and
// no Field. Msg after it is the provider's error, untouched.
const brokerAgentFailedPrefix = "broker protocol: agent_failed: "

// providerRefusalText returns the provider's own error message, unwrapping a
// relayed broker failure. Anything else is returned as it stands, so a string
// that merely mentions a refusal is compared as the whole string it is.
func providerRefusalText(err error) string {
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, brokerAgentFailedPrefix); ok {
		return rest
	}
	return msg
}

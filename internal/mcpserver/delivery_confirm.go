// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"fmt"
	"strings"
)

// 🎯T305 — delivery confirmation and never-briefed status.
//
// Silent start/send success is banned: a tool that returns ok while the
// pane still holds an empty ❯ or a collapsed paste chip leaves supervisors
// acting on a lie (agent_list "running" for a worker that never received
// its brief). These helpers are the pure oracle surface.

// AgentListStatus is the process/brief status column for agent_list.
//
//	stopped       — no live process
//	never_briefed — live process, zero confirmed turns (T305 / T90)
//	running       — live process that has begun at least one turn
const (
	AgentStatusStopped      = "stopped"
	AgentStatusNeverBriefed = "never_briefed"
	AgentStatusRunning      = "running"
)

// ClassifyAgentListStatus decides stopped | never_briefed | running.
// turnBegan is process-local evidence (successful start prompt or send).
// materialized is durable conversation evidence (registry Materialized /
// session JSONL). Either counts as "has been briefed".
func ClassifyAgentListStatus(alive, turnBegan, materialized bool) string {
	if !alive {
		return AgentStatusStopped
	}
	if turnBegan || materialized {
		return AgentStatusRunning
	}
	return AgentStatusNeverBriefed
}

// ConfirmSendBeganTurn returns nil when a send/start-prompt outcome means
// a turn actually began. Queued / interrupted_queued do not count — the
// text is not yet in the pane as a submitted turn.
func ConfirmSendBeganTurn(status string, sendErr error) error {
	if sendErr != nil {
		return sendErr
	}
	switch strings.TrimSpace(status) {
	case "sent", "rehydrated_sent", "interrupted_sent":
		return nil
	case "queued", "interrupted_queued":
		return fmt.Errorf("turn not begun: send status=%s (queued, not submitted)", status)
	case "":
		return fmt.Errorf("turn not begun: empty send status")
	default:
		return fmt.Errorf("turn not begun: unexpected send status=%s", status)
	}
}

// markAgentTurnBegan records that name has begun at least one turn in
// this daemon process (🎯T305 never_briefed vs running).
func (s *Server) markAgentTurnBegan(name string) {
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentTurnBegan == nil {
		s.agentTurnBegan = map[string]bool{}
	}
	s.agentTurnBegan[name] = true
}

// agentHasTurnBegan reports process-local turn evidence for name.
func (s *Server) agentHasTurnBegan(name string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentTurnBegan[name]
}

// clearAgentTurnBegan drops process-local evidence (on kill/remove). It also
// forgets what was known about the agent's turn (🎯T416): a seat that has gone
// away must not leave an in-flight record behind, or the next send to a
// re-minted agent of the same name is held in a queue that nothing will drain.
func (s *Server) clearAgentTurnBegan(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.agentTurnBegan, name)
	delete(s.agentFlight, name)
	s.mu.Unlock()
	// 🎯T426: the sink subscription is a claim about the same departed seat.
	// Outside the lock on purpose — the wiring mutex is never taken under mu
	// (see attachAgentSink on lock order).
	s.forgetAgentWiring(name)
	// 🎯T392.4: so is the depth of the turn that seat was running.
	s.forgetTurnDepth(name)
}

// 🎯T518 — queued / delivered_unconfirmed is a brief IN FLIGHT, not a brief
// that never landed.
//
// The reap this distinction gates: agents.go and spawnFrontierWorker answer a
// failed deliverStartPrompt with releaseUnbriefedSeat, which stops the process
// AND removes the registry row as unbriefed_seat. That teardown is right for a
// brief PROVEN to have gone nowhere (🎯T387's phantom seats: send reported
// sent, the agent did nothing, no queue holds anything). It is wrong for the
// two middle verdicts of the 🎯T416 contract:
//
//   - "queued": the daemon's own backlog (or the receiver's queue) HOLDS the
//     brief and delivers it at the next turn boundary. Destroying the seat
//     destroys the held brief with it — jv-t515-relayrecord was retired as
//     unbriefed_seat on 2026-08-18T11:16Z seven minutes after a start whose
//     brief sat exactly there, and the remint then raced the auto-spawn.
//   - "delivered_unconfirmed": the instrument could not decide. 🎯T416 says
//     treat it as undelivered FOR RE-SENDING; it has never meant proven-lost,
//     and a teardown on an undecided verdict is the daemon inventing the one
//     answer the instrument refused to give.
//
// The spawn still cannot claim prompt_delivered=true on these — the failure
// stays loud — but the seat stays registered, where the queue drain and the
// 🎯T418 sweep can finish the delivery. What still reaps is positive evidence
// of no brief: a clean "sent" whose watch saw no user message and no queue
// record (🎯T387), or a send error that disproves delivery.

// briefInFlightError marks a deliverStartPrompt failure whose verdict was
// queued / delivered_unconfirmed (🎯T518): the brief is held or undecided,
// never proven absent, so the seat must not be retired as unbriefed_seat.
type briefInFlightError struct {
	status string
	err    error
}

func (e *briefInFlightError) Error() string { return e.err.Error() }
func (e *briefInFlightError) Unwrap() error { return e.err }

// BriefInFlight reports whether err marks an in-flight opening brief (🎯T518).
func BriefInFlight(err error) bool {
	var b *briefInFlightError
	return errors.As(err, &b)
}

// StartVerdictInFlight is the pure classifier: a clean send whose status is
// one of the two middle answers of the 🎯T416 contract (plus the interrupt
// variant of queued) means the brief is accepted or undecided — in flight.
// A send error is never in flight here: ConfirmTurnBegan already accepts a
// non-disproving error when the receiver's own records show the payload
// (🎯T429), and an error without such evidence stays a reap.
func StartVerdictInFlight(status string, sendErr error) bool {
	if sendErr != nil {
		return false
	}
	switch strings.TrimSpace(status) {
	case "queued", "interrupted_queued", "delivered_unconfirmed":
		return true
	}
	return false
}

// startBriefFailureTeardown applies the 🎯T518 fork after a failed
// deliverStartPrompt: an in-flight verdict keeps the seat and reports
// kept=true; anything else releases it (🎯T387) and reports whether a row
// this call minted was retired. Both spawn call sites (jevons_agent_start
// and spawnFrontierWorker) go through here so the fork is one testable unit.
func (s *Server) startBriefFailureTeardown(name string, existed bool, err error) (released, kept bool) {
	if BriefInFlight(err) {
		return false, true
	}
	return s.releaseUnbriefedSeat(name, existed), false
}

// deliverStartPrompt injects the optional jevons_agent_start prompt after
// Launch and requires confirmed turn begin (🎯T305 Failure A). Empty
// prompt is a no-op (start-only seats stay never_briefed until first send).
//
// A failure that is really an in-flight brief (queued / delivered_unconfirmed)
// comes back as a briefInFlightError (🎯T518): still an error — the turn has
// not begun — but one the caller must answer by keeping the seat.
func (s *Server) deliverStartPrompt(name, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil
	}
	s.mu.Lock()
	if s.fleetBriefed == nil {
		s.fleetBriefed = map[string]bool{}
	}
	roleBody := ""
	if s.registry != nil {
		if d := s.registry.Def(name); d != nil {
			roleName := s.roleDisplay(*d)
			if def, err := s.resolveRoleDef(roleName); err == nil {
				roleBody = def.Body
			}
		}
	}
	text, injected := EnsureFleetBriefWithRole(s.fleetBriefed, name, prompt, roleBody)
	s.mu.Unlock()
	if injected {
		// Same inject-once as first agent_send so start-with-prompt does
		// not double the standing brief on a later send.
	}
	// 🎯T425: a spawn opening prompt is daemon-composed and is the first thing
	// this seat ever reads — the one message where "who am I" is least
	// answerable from anything already in the session.
	text = s.withIdentity(name, text)

	// 🎯T387: open the observation BEFORE the send, so transcript growth is
	// measured against a pre-send baseline rather than against whatever an
	// earlier session left on disk. See turn_evidence.go for why the send
	// result alone was never evidence of anything about the agent.
	// 🎯T416 finding 3 — AND IT LOOKS FOR THIS PAYLOAD, not merely for signs of
	// life. This line used to be watchAgentTurn(name), i.e. growth-based
	// confirmation, on the licence that "a seat this daemon just launched has
	// an empty composer and an empty transcript, so nothing else can be writing
	// the growth it sees."
	//
	// TRUE FOR A FRESH SEAT, VOID FOR A RESUMED ONE, and the spawn path
	// launches both. Counter-example, live, one hour before this was written:
	// this target's own worker was stopped mid-turn at 19:32:08 and restarted
	// at 19:32:18 onto the SAME session, which already held 158 lines on disk.
	// At 19:32:20 the daemon logged prompt_delivered=true — while the payload
	// sat unsubmitted in the composer as `[Pasted text #1 +10 lines]`, and the
	// transcript gained nothing for the next four minutes until a human pressed
	// Enter. A resumed session's startup writes are "a sign of life", so growth
	// confirmed a delivery that had not happened: clause 9's EXCLUDED mutant
	// (i), alive on the start path.
	//
	// The fix is to stop asking the easier question rather than to qualify when
	// it may be asked. The payload is right here; passing it removes the
	// licence argument entirely and makes the start path answer the same
	// question as the other four callers.
	watch := s.watchAgentTurnFor(name, text)

	// This watch owns the verdict (🎯T416): the send path must not also judge,
	// or a healthy brief comes back with a status ConfirmTurnBegan does not
	// recognise.
	res, err := s.deliverByNameConfirmedByCaller(name, text, OriginAgent, false)
	if confErr := ConfirmTurnBegan(res.Status, err, watch()); confErr != nil {
		// 🎯T518: the two middle verdicts mean the brief is HELD — by this
		// daemon's queue or by the receiver — or the instrument could not
		// decide. Neither is "never landed". clearAgentTurnBegan is skipped on
		// purpose: it deletes the flight record the queued delivery drains
		// through, and this seat has no false turn-began mark to remove (the
		// queued arm never set one).
		if StartVerdictInFlight(res.Status, err) {
			return &briefInFlightError{status: res.Status, err: fmt.Errorf(
				"start brief to %q in flight, not yet a turn (status=%s): %w",
				name, res.Status, confErr)}
		}
		// deliverToSender marks turn-began off its own successful Send, which
		// is the same send-call inference this target exists to remove. The
		// spawn owns this agent's first turn, so it must not leave behind a
		// mark it did not earn — otherwise agent_list reports "running" for a
		// worker whose brief demonstrably never landed (🎯T387).
		s.clearAgentTurnBegan(name)
		return fmt.Errorf("start prompt not delivered to %q: %w", name, confErr)
	}
	s.markAgentTurnBegan(name)
	return nil
}

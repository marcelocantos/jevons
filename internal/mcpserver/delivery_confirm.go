// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
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
}

// deliverStartPrompt injects the optional jevons_agent_start prompt after
// Launch and requires confirmed turn begin (🎯T305 Failure A). Empty
// prompt is a no-op (start-only seats stay never_briefed until first send).
func (s *Server) deliverStartPrompt(name, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil
	}
	s.mu.Lock()
	if s.fleetBriefed == nil {
		s.fleetBriefed = map[string]bool{}
	}
	text, injected := EnsureFleetBrief(s.fleetBriefed, name, prompt)
	s.mu.Unlock()
	if injected {
		// Same inject-once as first agent_send so start-with-prompt does
		// not double the standing brief on a later send.
	}

	// 🎯T387: open the observation BEFORE the send, so transcript growth is
	// measured against a pre-send baseline rather than against whatever an
	// earlier session left on disk. See turn_evidence.go for why the send
	// result alone was never evidence of anything about the agent.
	watch := s.watchAgentTurn(name)

	// This watch owns the verdict (🎯T416): the send path must not also judge,
	// or a healthy brief comes back with a status ConfirmTurnBegan does not
	// recognise. The spawn asks the easier question and is entitled to: a seat
	// this daemon just launched has an empty composer and an empty transcript,
	// so nothing else can be writing the growth it sees.
	res, err := s.deliverByNameConfirmedByCaller(name, text, OriginAgent, false)
	if confErr := ConfirmTurnBegan(res.Status, err, watch()); confErr != nil {
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

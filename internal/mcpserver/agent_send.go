// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleet"
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
func (s *Server) enqueueAgentSend(name, text string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentSendQ == nil {
		s.agentSendQ = map[string][]string{}
	}
	s.agentSendQ[name] = append(s.agentSendQ[name], text)
	return len(s.agentSendQ[name])
}

// dequeueAgentSend pops the oldest pending message, or "" if empty.
func (s *Server) dequeueAgentSend(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentSendQ == nil {
		return ""
	}
	q := s.agentSendQ[name]
	if len(q) == 0 {
		return ""
	}
	text := q[0]
	if len(q) == 1 {
		delete(s.agentSendQ, name)
	} else {
		s.agentSendQ[name] = q[1:]
	}
	return text
}

// pendingAgentSends returns queue depth for name.
func (s *Server) pendingAgentSends(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentSendQ == nil {
		return 0
	}
	return len(s.agentSendQ[name])
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
		if reps := SweepDeadAgents(s.registry, s.overseerName()); len(reps) > 0 {
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

// deliverToSender is the pure-ish busy/queue/interrupt path used by
// sendToAgent and hermetic tests with a fake sender.
func deliverToSender(s *Server, name, text string, interrupt bool, proc agentSender, rehydrated bool) (agentSendResult, error) {
	if proc == nil || !proc.Alive() {
		return agentSendResult{}, fmt.Errorf("agent %q is not running", name)
	}

	trySend := func() error { return proc.Send(text) }

	err := trySend()
	if err == nil {
		status := "sent"
		msg := fmt.Sprintf("Message sent to %q. You will be notified when it responds.", name)
		if rehydrated {
			status = "rehydrated_sent"
			msg += " (rehydrated after dead/stopped process)"
		}
		res := agentSendResult{Status: status, Message: msg, Queued: s.pendingAgentSends(name)}
		logAgentSendResult(name, res, rehydrated)
		// 🎯T305: confirmed Send (incl. paste-block press-through in claudia)
		// means a turn began — never_briefed → running for agent_list.
		if s != nil {
			s.markAgentTurnBegan(name)
		}
		return res, nil
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
			n := s.enqueueAgentSend(name, text)
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
		if err2 := trySend(); err2 == nil {
			msg := fmt.Sprintf("Interrupted in-flight turn on %q and sent the new message.", name)
			res := agentSendResult{Status: "interrupted_sent", Message: msg, Queued: s.pendingAgentSends(name)}
			logAgentSendResult(name, res, rehydrated)
			if s != nil {
				s.markAgentTurnBegan(name)
			}
			return res, nil
		} else if isPromptInFlight(err2) {
			n := s.enqueueAgentSend(name, text)
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

	n := s.enqueueAgentSend(name, text)
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

// drainAgentSendQueue delivers the next queued message after a terminal
// stop. Called from the agent event sink (🎯T111.1).
func (s *Server) drainAgentSendQueue(name string) {
	text := s.dequeueAgentSend(name)
	if text == "" {
		return
	}
	if s.registry == nil {
		return
	}
	proc := s.registry.Get(name)
	if proc == nil || !proc.Alive() {
		// Put back — process gone mid-queue; next send/rehydrate can retry.
		s.enqueueAgentSend(name, text)
		slog.Warn("agent send queue: process not alive; re-queued", "name", name)
		return
	}
	if err := proc.Send(text); err != nil {
		if isPromptInFlight(err) {
			// Still busy (nested tool pause edge): put at front by re-queue.
			s.mu.Lock()
			if s.agentSendQ == nil {
				s.agentSendQ = map[string][]string{}
			}
			s.agentSendQ[name] = append([]string{text}, s.agentSendQ[name]...)
			s.mu.Unlock()
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
	slog.Info("agent send queue: drained one message", "name", name, "remaining", s.pendingAgentSends(name))
}

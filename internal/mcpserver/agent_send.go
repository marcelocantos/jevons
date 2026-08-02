// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
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

// isPromptInFlight reports the Grok ACP busy error that dead-ended
// overseer nudges before 🎯T111.1.
func isPromptInFlight(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "already in flight")
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

// sendToAgent rehydrates if needed, optionally interrupts a busy turn,
// sends text, or queues when the prompt is already in flight (🎯T111.1).
// Does not inject the fleet standing brief — callers that need it
// (handleAgentSend) apply EnsureFleetBrief first.
func (s *Server) sendToAgent(name, text string, interrupt bool) (agentSendResult, error) {
	if s.registry == nil {
		return agentSendResult{}, fmt.Errorf("agent registry not available")
	}
	if name == "" || text == "" {
		return agentSendResult{}, fmt.Errorf("name and text are required")
	}

	proc, rehydrated, err := s.ensureAgentProcess(name)
	if err != nil {
		return agentSendResult{}, err
	}
	return deliverToSender(s, name, text, interrupt, proc, rehydrated)
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
		return res, nil
	}

	if !isPromptInFlight(err) {
		return agentSendResult{}, fmt.Errorf("send failed: %v", err)
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
			return agentSendResult{}, fmt.Errorf("send after interrupt failed: %v", err2)
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
		slog.Warn("agent send queue: drain send failed", "name", name, "err", err)
		return
	}
	slog.Info("agent send queue: drained one message", "name", name, "remaining", s.pendingAgentSends(name))
}

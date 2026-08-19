// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// classifySend wraps ClassifySendOutcome with 🎯T417 honesty:
//
//  1. Mid-turn / composing: an idle-shaped "not submitted" is demoted to
//     unconfirmed — absence while the receiver is composing must not
//     false-negative as a stuck paste.
//  2. Prior durable delivery evidence: a confirmation recorded before a
//     later compaction still answers "was this delivered", even when the
//     transcript no longer carries the payload.
func (s *Server) classifySend(name, payload string, flight TurnFlight, ev TurnEvidence) SendOutcome {
	outcome := ClassifySendOutcome(flight, ev)
	if outcome == OutcomeBegun {
		return outcome
	}
	if s.priorDelivery(name, payload) {
		return OutcomeBegun
	}
	if outcome == OutcomeNotSubmitted && s.agentIsComposing(name) {
		return OutcomeUnconfirmed
	}
	return outcome
}

// agentIsComposing reports whether name is mid-turn (🎯T417). FlightInFlight
// is process-local; idleActivity phase=working covers the ACP-tracked case.
func (s *Server) agentIsComposing(name string) bool {
	if s == nil || strings.TrimSpace(name) == "" {
		return false
	}
	if s.flightState(name) == FlightInFlight {
		return true
	}
	if s.idleActivity != nil {
		if ph := strings.ToLower(strings.TrimSpace(s.idleActivity.Get(name).Phase)); ph == "working" {
			return true
		}
	}
	return false
}

// deliveryStateDir is where confirmed deliveries persist (🎯T417). Prefer the
// recover/stateDir wire; fall back to the agent-report root — both are the
// daemon state dir on the product path.
func (s *Server) deliveryStateDir() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	dir := strings.TrimSpace(s.stateDir)
	s.mu.Unlock()
	if dir != "" {
		return dir
	}
	return s.agentReportStateDir()
}

// priorDelivery consults the durable delivery-evidence store (🎯T417).
func (s *Server) priorDelivery(name, payload string) bool {
	dir := s.deliveryStateDir()
	if dir == "" || strings.TrimSpace(payload) == "" {
		return false
	}
	ok, err := delivery.WasDelivered(dir, name, payload)
	if err != nil {
		slog.Debug("delivery evidence lookup failed", "agent", name, "err", err)
		return false
	}
	return ok
}

// recordDeliveryEvidence persists a confirmed begun send so later compaction
// cannot erase the answer (🎯T417).
func (s *Server) recordDeliveryEvidence(name, payload string, ev TurnEvidence) {
	dir := s.deliveryStateDir()
	if dir == "" {
		return
	}
	if delivery.Needle(payload) == "" {
		return
	}
	sessionID := ""
	if s.registry != nil {
		if def := s.registry.Def(name); def != nil {
			sessionID = def.SessionID
		}
	}
	detail := strings.TrimSpace(ev.Detail)
	if detail == "" {
		detail = "confirmed begun"
	}
	if _, err := delivery.RecordPayload(dir, name, sessionID, payload, detail, "begun", time.Now()); err != nil {
		slog.Warn("delivery evidence not stored",
			"component", "agent_send", "agent", name, "err", err)
		return
	}
}

// ReadingForPayload is the reader-facing 🎯T417 answer: consult durable
// evidence first, then the transcript fate, with mid-turn honesty.
func (s *Server) ReadingForPayload(name, payload string, fate turnev.Fate) turnev.Reading {
	if s.priorDelivery(name, payload) {
		return turnev.ReadingDelivered
	}
	return turnev.ReadingFor(fate, s.agentIsComposing(name))
}

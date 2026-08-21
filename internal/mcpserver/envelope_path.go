// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"time"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// applyEnvelopeControls validates a load-bearing envelope on the deliver
// path (🎯T509) and applies chatter policy. drop=true means the original
// message is not sent; a notice may already have gone in a prior call.
func (s *Server) applyEnvelopeControls(actor, text string) (out string, drop bool) {
	m, err := envelope.Parse(text)
	if m == nil {
		return text, false
	}
	if err != nil && m.Kind.LoadBearing() {
		slog.Warn("T509 malformed load-bearing envelope",
			"actor", actor, "kind", m.Kind.String(), "err", err.Error())
		s.logLifecycle(compAgentLifecycle, "envelope", "malformed", map[string]any{
			"actor": actor, "kind": m.Kind.String(), "err": err.Error(),
		})
		text = envelope.Annotate(text, err)
	}
	obs := s.chatterTracker().Check(actor, m, time.Now())
	if obs.Action == envelope.ActionDeliver {
		return text, false
	}
	slog.Info("T509 envelope chatter",
		"actor", actor, "kind", m.Kind.String(), "action", obs.Action.String(), "count", obs.Count)
	s.logLifecycle(compAgentLifecycle, "envelope", obs.Action.String(), map[string]any{
		"actor": actor, "kind": m.Kind.String(), "count": obs.Count,
	})
	if obs.Notice != "" {
		return obs.Notice, false
	}
	return text, true
}

func (s *Server) chatterTracker() *envelope.Tracker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.envelopeChatter == nil {
		s.envelopeChatter = envelope.NewTracker()
	}
	return s.envelopeChatter
}

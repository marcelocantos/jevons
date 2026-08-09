// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"

	"github.com/marcelocantos/jevons/internal/provider"
)

// Automation liveness observability (🎯T27.9): GET /api/automations
// reports every tracked automation's state (ok/stalled/unknown) from the
// LivenessMonitor. Stall/recovery transitions additionally reach clients
// as provider_event broadcasts and the owner via the overseer notify
// path — this endpoint is the pollable snapshot.

// SetAutomations attaches the liveness status source. Nil (never wired)
// leaves /api/automations reporting an empty tracked set.
func (s *Server) SetAutomations(fn func() []provider.AutomationStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.automations = fn
}

func (s *Server) handleAutomations(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	fn := s.automations
	s.mu.RUnlock()
	list := []provider.AutomationStatus{}
	if fn != nil {
		if a := fn(); a != nil {
			list = a
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"automations": list})
}

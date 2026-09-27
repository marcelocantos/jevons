// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
)

// reauthablePlanFailure is a presentation hint, not the broker's auth gate.
// Offer recovery on a stopped broken plan seat regardless of error wording:
// a failed Keychain read can surface as an OS error with no "keychain" token.
// The broker decides whether to retry the read, refresh, or open sign-in.
func reauthablePlanFailure(provider claudia.Provider, health string) bool {
	if !strings.HasPrefix(health, "broken:") {
		return false
	}
	switch provider {
	case "anthropic", "openai-codex", "grok", "xai-oauth", "cursor":
		return true
	default:
		return false
	}
}

// handleAgentAuthRecover asks Claudia's running broker to repair one plan login.
// It never attempts to open the Keychain in the Jevons daemon.
func (s *Server) handleAgentAuthRecover(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "agent name is required")
		return
	}
	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()
	if reg == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "agent registry is unavailable")
		return
	}
	def := reg.Def(name)
	if def == nil {
		writeJSONError(w, http.StatusNotFound, "agent is no longer registered")
		return
	}
	if proc := reg.Get(name); proc != nil && proc.Alive() {
		writeJSONError(w, http.StatusConflict, "agent is already running")
		return
	}
	if !reauthablePlanFailure(def.Provider, fleet.RehydrateHealth(*def)) {
		writeJSONError(w, http.StatusConflict, "agent no longer has a plan authentication failure")
		return
	}
	recoverAuth := s.authRecover
	if recoverAuth == nil {
		recoverAuth = runClaudiaAuthRecover
	}
	if err := recoverAuth(r.Context(), def.Provider); err != nil {
		if proc := reg.Get(name); proc == nil || !proc.Alive() {
			fleet.RecordRehydrateFailure(name, fmt.Errorf("auth recovery: %w", err))
		}
		s.NotifyAgentsChanged()
		writeJSONError(w, http.StatusBadGateway, "Claudia could not recover authentication: "+err.Error())
		return
	}
	if proc := reg.Get(name); proc != nil && proc.Alive() {
		s.NotifyAgentsChanged()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "already_running"})
		return
	}
	current := reg.Def(name)
	if current == nil || current.Provider != def.Provider {
		s.NotifyAgentsChanged()
		writeJSONError(w, http.StatusConflict, "agent changed while authentication was being recovered")
		return
	}
	if _, err := fleet.LaunchReconciled(reg, name); err != nil {
		s.NotifyAgentsChanged()
		writeJSONError(w, http.StatusBadGateway, "Authentication recovered, but the agent did not start: "+err.Error())
		return
	}
	s.NotifyAgentsChanged()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "running"})
}

func runClaudiaAuthRecover(ctx context.Context, provider claudia.Provider) error {
	// The installed Claudia CLI is the versioned control-plane client. Keep
	// Jevons buildable against its pinned Claudia module while the broker
	// protocol rolls out with the next Claudia binary.
	out, err := exec.CommandContext(ctx, "claudia", "broker", "auth-recover", string(provider)).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("Claudia auth recovery failed: %s", message)
	}
	return nil
}

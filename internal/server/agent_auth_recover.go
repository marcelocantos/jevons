// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/planusage"
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
	// The Claudia CLI is the versioned control-plane client. An explicit
	// CLAUDIA_BIN pin keeps development on the same broker/client build;
	// the default is the installed release. Jevons remains buildable against
	// its pinned Claudia module while the broker protocol rolls out.
	bin := strings.TrimSpace(os.Getenv("CLAUDIA_BIN"))
	if bin == "" {
		bin = "claudia"
	}
	out, err := exec.CommandContext(ctx, bin, "broker", "auth-recover", string(provider)).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("Claudia auth recovery failed: %s", message)
	}
	return nil
}

// recoverableDestinationProvider maps Claudia's plan identity to the
// sidecar subscription id accepted by its auth-recovery broker request.
func recoverableDestinationProvider(provider claudia.Provider) claudia.Provider {
	id := claudia.SubscriptionSeatProvider(claudia.PlanProvider(provider))
	switch id {
	case "anthropic", "openai-codex", "cursor", "xai-oauth":
		return id
	default:
		return ""
	}
}

// destinationAuthRecoverable is only a presentation/action hint for an
// already-failed migration. It never judges credential validity itself; the
// Claudia broker does that when the owner explicitly asks it to recover.
func destinationAuthRecoverable(action planusage.PlanAction) bool {
	if action.Action != claudia.SeatMigrate || action.Execution != "failed" ||
		recoverableDestinationProvider(claudia.Provider(action.To)) == "" {
		return false
	}
	failure := strings.ToLower(action.Failure)
	for _, marker := range []string{
		"invalid_grant", "refresh token", "keychain", "authentication failed",
		"login required", "not logged in", "no login",
	} {
		if strings.Contains(failure, marker) {
			return true
		}
	}
	return false
}

// handlePlanDestinationAuthRecover repairs the destination login of a failed
// move while leaving its running source seat alone. The periodic plan sweep
// retries the same Claudia-owned migration after recovery.
func (s *Server) handlePlanDestinationAuthRecover(w http.ResponseWriter, r *http.Request) {
	provider := recoverableDestinationProvider(claudia.Provider(strings.TrimSpace(r.PathValue("provider"))))
	if provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider has no recoverable subscription login")
		return
	}
	if s.planDecisions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "plan decisions are unavailable")
		return
	}
	matched := false
	for _, action := range s.planDecisions() {
		if recoverableDestinationProvider(claudia.Provider(action.To)) == provider && destinationAuthRecoverable(action) {
			matched = true
			break
		}
	}
	if !matched {
		writeJSONError(w, http.StatusConflict, "no current migration has a destination authentication failure on this provider")
		return
	}
	if s.planSweep == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "migration retry is unavailable")
		return
	}
	recoverAuth := s.authRecover
	if recoverAuth == nil {
		recoverAuth = runClaudiaAuthRecover
	}
	if err := recoverAuth(r.Context(), provider); err != nil {
		writeJSONError(w, http.StatusBadGateway, "Claudia could not recover authentication: "+err.Error())
		return
	}
	// The broker has repaired the destination login. Retry from the current
	// Claudia registry state without holding the HTTP request through a paid
	// context transfer and provider start.
	go s.planSweep()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "recovered", "provider": string(provider), "migration": "retry_pending",
	})
}

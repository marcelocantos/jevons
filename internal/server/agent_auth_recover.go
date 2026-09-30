// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// reauthablePlanFailure is a presentation hint, not the broker's auth gate.
// Offer recovery on a stopped broken plan seat regardless of error wording:
// a failed Keychain read can surface as an OS error with no "keychain" token.
// The broker decides whether to retry the read, refresh, or open sign-in.
func reauthablePlanFailure(provider claudia.Provider, health string) bool {
	return strings.HasPrefix(health, "broken:") && reauthablePlanProvider(provider)
}

func reauthablePlanProvider(provider claudia.Provider) bool {
	switch provider {
	case "anthropic", "openai-codex", "grok", "xai-oauth", "cursor":
		return true
	default:
		return false
	}
}

// runningPlanAuthFailure reports a running seat whose turns its provider
// refuses on the plan login — a revoked token (🎯T905).
func (s *Server) runningPlanAuthFailure(def claudia.AgentDef) bool {
	return s.planAuthFailed != nil && reauthablePlanProvider(def.Provider) && s.planAuthFailed(def.Name)
}

// decoratePlanAuth offers Reauth on a running seat whose provider refuses
// its login (🎯T905); a stopped one is offered it by its rehydrate health.
func (s *Server) decoratePlanAuth(reg *claudia.Registry, agents []agentInfo) []agentInfo {
	if s.planAuthFailed == nil || reg == nil {
		return agents
	}
	for i := range agents {
		if !agents[i].Running {
			continue
		}
		if def := reg.Def(agents[i].Name); def != nil && s.runningPlanAuthFailure(*def) {
			agents[i].ReauthAvailable = true
		}
	}
	return agents
}

// notePlanAuthRecovered forgets the login refusals of every registered seat
// on provider's plan, now that the owner has repaired it (🎯T905).
func (s *Server) notePlanAuthRecovered(reg *claudia.Registry, provider claudia.Provider) {
	if s.planAuthRecovered == nil || reg == nil {
		return
	}
	plan := recoverableDestinationProvider(provider)
	var names []string
	for _, d := range reg.List() {
		if d.Provider == provider || (plan != "" && recoverableDestinationProvider(d.Provider) == plan) {
			names = append(names, d.Name)
		}
	}
	s.planAuthRecovered(names)
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
		s.recoverRunningSeatAuth(w, r, reg, *def)
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
	// The repaired login is the plan's: running seats refused on it are
	// answered too, and the plan bar re-reads its status (🎯T945).
	s.forgetPlanAuthStatus()
	s.notePlanAuthRecovered(reg, def.Provider)
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
	// The login is the plan's, not this seat's: every seat that broke on it
	// can come back now, whether or not this one starts.
	if s.planAuthRevive != nil {
		defer func() {
			go func() {
				s.planAuthRevive(def.Provider, name)
				s.NotifyAgentsChanged()
			}()
		}()
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

// recoverRunningSeatAuth repairs the plan login of a seat that is still
// running but refused on it (🎯T905). Claudia reloads the seats running on
// that plan with the recovered token, so the seat is not relaunched: its
// next turn runs on the repaired login.
func (s *Server) recoverRunningSeatAuth(w http.ResponseWriter, r *http.Request, reg *claudia.Registry, def claudia.AgentDef) {
	if !s.runningPlanAuthFailure(def) {
		writeJSONError(w, http.StatusConflict, "agent is already running")
		return
	}
	recoverAuth := s.authRecover
	if recoverAuth == nil {
		recoverAuth = runClaudiaAuthRecover
	}
	if err := recoverAuth(r.Context(), def.Provider); err != nil {
		s.NotifyAgentsChanged()
		writeJSONError(w, http.StatusBadGateway, "Claudia could not recover authentication: "+err.Error())
		return
	}
	s.notePlanAuthRecovered(reg, def.Provider)
	// Seats that broke on the same login and stopped come back too.
	if s.planAuthRevive != nil {
		go func() {
			s.planAuthRevive(def.Provider, def.Name)
			s.NotifyAgentsChanged()
		}()
	}
	s.NotifyAgentsChanged()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "recovered"})
}

// runningAuthRecoverCooldown spaces automatic recovery calls for one
// provider's plan, so a plan that is still broken is not hammered once per
// standing-sweep tick.
const runningAuthRecoverCooldown = 2 * time.Minute

var runningAuthRecoverTried sync.Map // provider -> time.Time

// RecoverRunningPlanAuthFailures is the standing-sweep half of 🎯T943: a
// running seat refused on a revoked plan token never retries its own login,
// so nothing repairs it once a sibling seat's success (or an owner's repair
// through a different seat) has already cleared the plan for everyone else.
// The 2026-09-29 owner-click path (recoverRunningSeatAuth) only reaches a
// seat the owner happens to click Reauth on; this reaches every running seat
// still marked plan-auth-failed, the same way RevivePlanAuthWhereHealthy
// reaches every stopped one. On 2026-09-30 jv-t943's specimen (jevons-po)
// sat "running idle" for twelve minutes after jevons and claudia-po had
// already completed turns on the same anthropic plan.
func (s *Server) RecoverRunningPlanAuthFailures(ctx context.Context) []string {
	if s == nil || s.planAuthFailed == nil {
		return nil
	}
	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()
	if reg == nil {
		return nil
	}
	byProvider := map[claudia.Provider][]string{}
	for _, d := range reg.List() {
		if d.Name == "" || !s.planAuthFailed(d.Name) {
			continue
		}
		if proc := reg.Get(d.Name); proc == nil || !proc.Alive() {
			continue
		}
		byProvider[d.Provider] = append(byProvider[d.Provider], d.Name)
	}
	if len(byProvider) == 0 {
		return nil
	}
	recoverAuth := s.authRecover
	if recoverAuth == nil {
		recoverAuth = runClaudiaAuthRecover
	}
	now := time.Now()
	var recovered []string
	for provider, names := range byProvider {
		if last, ok := runningAuthRecoverTried.Load(provider); ok && now.Sub(last.(time.Time)) < runningAuthRecoverCooldown {
			continue
		}
		runningAuthRecoverTried.Store(provider, now)
		if err := recoverAuth(ctx, provider); err != nil {
			slog.Warn("running-seat plan auth recovery failed", "provider", provider, "seats", names, "err", err)
			continue
		}
		runningAuthRecoverTried.Delete(provider)
		s.forgetPlanAuthStatus()
		s.notePlanAuthRecovered(reg, provider)
		recovered = append(recovered, names...)
		slog.Info("recovered running seats' plan login without a relaunch", "provider", provider, "seats", names)
	}
	if len(recovered) > 0 {
		s.NotifyAgentsChanged()
	}
	return recovered
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

// handlePlanDestinationAuthRecover repairs a plan login on the owner's click:
// the destination of a failed move, or any plan whose login the broker
// reports missing, expired or rejected (🎯T924). A running source seat is
// left alone, and the periodic plan sweep retries a failed migration after.
func (s *Server) handlePlanDestinationAuthRecover(w http.ResponseWriter, r *http.Request) {
	provider := recoverableDestinationProvider(claudia.Provider(strings.TrimSpace(r.PathValue("provider"))))
	if provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider has no recoverable subscription login")
		return
	}
	var decisions []planusage.PlanAction
	if s.planDecisions != nil {
		decisions = s.planDecisions()
	}
	matched := false
	for _, action := range decisions {
		if recoverableDestinationProvider(claudia.Provider(action.To)) == provider && destinationAuthRecoverable(action) {
			matched = true
			break
		}
	}
	if !matched && !s.planLoginUnhealthy(r.Context(), provider) {
		writeJSONError(w, http.StatusConflict, "this provider's plan login is healthy and no migration is waiting on it")
		return
	}
	if matched && s.planSweep == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "migration retry is unavailable")
		return
	}
	recoverAuth := s.authRecover
	if recoverAuth == nil {
		recoverAuth = runClaudiaAuthRecover
	}
	err := recoverAuth(r.Context(), provider)
	s.forgetPlanAuthStatus()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "Claudia could not recover authentication: "+err.Error())
		return
	}
	// The failures that offered this action are now answered; say so before
	// the retry sweep, which can take minutes of context transfer, reports.
	if s.planRetryAfterReauth != nil {
		s.planRetryAfterReauth(provider)
	}
	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()
	s.notePlanAuthRecovered(reg, provider) // 🎯T905
	if s.planAuthRevive != nil {
		go func() {
			s.planAuthRevive(provider, "")
			s.NotifyAgentsChanged()
		}()
	}
	if !matched {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "recovered", "provider": string(provider)})
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

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/marcelocantos/claudia"
)

// ModelSwitcher moves a seat to another provider or model in place,
// keeping its conversation (claudia 🎯T195). Implemented by *fleet.Claudia;
// nil leaves POST /api/agents/switch-model unavailable rather than
// half-wired.
type ModelSwitcher interface {
	SwitchModel(name string, to claudia.Provider, model string, force bool) (claudia.SeatSwitch, error)
}

// SetModelSwitcher wires the in-place model switch behind
// POST /api/agents/switch-model.
func (s *Server) SetModelSwitcher(m ModelSwitcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelSwitcher = m
}

// handleSwitchModel is POST /api/agents/switch-model — the owner's
// per-seat provider/model choice from the fleet tree (🎯T285.2), for every
// seat including the overseer. Body: {"name":…, "provider":…, "model":…,
// "force":…}; an empty name is the overseer, an empty provider keeps the
// seat's own, and an empty model takes the destination's standard model.
//
// The seat switches between turns and keeps its name, session and
// conversation, so owner chat stays attached to the overseer: there is no
// relaunch to re-attach and no handover to seed. A turn in flight refuses
// the switch unless force is set; for the overseer, force ends the owner
// turn first, since a fleet reporting to it every minute leaves no gap.
func (s *Server) handleSwitchModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Force    bool   `json:"force"`
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	s.mu.RLock()
	sw := s.modelSwitcher
	overseer := s.overseerName
	s.mu.RUnlock()
	if sw == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "model switching is not wired")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = overseer
	}
	provider := strings.ToLower(strings.TrimSpace(body.Provider))
	model := strings.TrimSpace(body.Model)
	if provider == "" && model == "" {
		writeJSONError(w, http.StatusBadRequest, "provider or model is required")
		return
	}
	if body.Force && name == overseer {
		slog.Warn("forced overseer switch: ending the owner turn first", "to", provider, "model", model)
		s.interruptOwnerTurn()
	}
	got, err := sw.SwitchModel(name, claudia.Provider(provider), model, body.Force)
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "name": name, "live": got.Live, "changed": got.Changed,
		"from_provider": string(got.From), "provider": string(got.To),
		"from_model": got.FromModel, "model": got.Model,
	})
	if got.Changed {
		s.NotifyAgentsChanged()
	}
}

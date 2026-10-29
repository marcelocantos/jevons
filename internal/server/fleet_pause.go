// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// FleetPauseControl is the owner cockpit fleet-wide pause toggle (🎯T966).
type FleetPauseControl struct {
	Pause   func(actor, reason string) error
	Resume  func(actor, reason string) error
	Paused  func() bool
}

// SetFleetPauseControl installs cockpit fleet pause/resume.
func (s *Server) SetFleetPauseControl(c FleetPauseControl) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fleetPauseControl = c
}

type fleetPauseRequest struct {
	Pause  bool   `json:"pause"`
	Reason string `json:"reason"`
}

type fleetPauseResponse struct {
	Paused bool `json:"paused"`
}

func (s *Server) handleFleetPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if rejectCrossSite(w, r) {
		return
	}
	var body fleetPauseRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	s.mu.RLock()
	ctrl := s.fleetPauseControl
	s.mu.RUnlock()
	if ctrl.Pause == nil || ctrl.Resume == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "fleet pause is not available")
		return
	}
	reason := strings.TrimSpace(body.Reason)
	var err error
	if body.Pause {
		err = ctrl.Pause("owner", reason)
	} else {
		err = ctrl.Resume("owner", reason)
	}
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(fleetPauseResponse{Paused: ctrl.Paused()})
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// SeatPauseControl is the product hook for per-seat pause (🎯T981).
type SeatPauseControl struct {
	Pause   func(name string) error
	Unpause func(name string) error
	Paused  func(name string) bool
}

// SetSeatPauseControl installs cockpit pause/unpause (mcpserver in production).
func (s *Server) SetSeatPauseControl(c SeatPauseControl) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seatPauseControl = c
}

type seatPauseResponse struct {
	Name   string `json:"name"`
	Status string `json:"status"` // paused | running
}

func (s *Server) handleAgentPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "agent name is required")
		return
	}
	s.mu.RLock()
	ctrl := s.seatPauseControl
	s.mu.RUnlock()
	if ctrl.Pause == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "seat pause is not available")
		return
	}
	if err := ctrl.Pause(name); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	s.NotifyAgentsChanged()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(seatPauseResponse{Name: name, Status: "paused"})
}

func (s *Server) handleAgentUnpause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "agent name is required")
		return
	}
	s.mu.RLock()
	ctrl := s.seatPauseControl
	s.mu.RUnlock()
	if ctrl.Unpause == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "seat pause is not available")
		return
	}
	if err := ctrl.Unpause(name); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	s.NotifyAgentsChanged()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(seatPauseResponse{Name: name, Status: "running"})
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// SetCapacitySource registers the provider of the live background-work
// admission picture served at GET /api/capacity (🎯T359).
func (s *Server) SetCapacitySource(f func() any) { s.capacitySource = f }

// handleCapacity serves the capacity governor's status: current pressure,
// remaining headroom by dimension, per-class in-flight counts, and what each
// background class would be granted if it asked right now.
//
// Unwired (no cost spine, or admission control switched off in
// capacity.json) reports disabled rather than an error — the same honesty
// shape as GET /api/cost.
func (s *Server) handleCapacity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.capacitySource == nil {
		w.Write([]byte(`{"disabled":true,"error":"capacity admission control not enabled"}`))
		return
	}
	status := s.capacitySource()
	if status == nil {
		w.Write([]byte(`{"disabled":true,"error":"no capacity snapshot yet"}`))
		return
	}
	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.Warn("encode capacity status", "err", err)
	}
}

// OwnerTurnInFlight reports whether the overseer is mid-turn on an owner
// prompt. Background admission reads this so ambient cycles yield the seat
// while the owner is waiting (🎯T291 / 🎯T359).
func (s *Server) OwnerTurnInFlight() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiting && s.overseerOwnerTurn
}

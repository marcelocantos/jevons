// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// SetFleetHealthSource registers the provider of the live PO-fanout picture
// served at GET /api/fleet-health (🎯T713). Same honesty shape as
// GET /api/capacity: unwired reports disabled rather than inventing healthy.
func (s *Server) SetFleetHealthSource(f func() any) { s.fleetHealthSource = f }

// handleFleetHealth serves the sentinel's own PO-fanout verdicts over HTTP so
// an unattended supervisor can ask without chat or filesystem reach (🎯T713).
//
// The body carries every product owner scoped to the sampled ledger, with
// verdict + reason in the T586 vocabulary (idle_on_ready_leaves,
// capacity_refused, provider_exhausted, all_gated, turn_ended_zero_new_children,
// …), a computed_at timestamp, and stale=true when the reading is too old to
// treat as current. A stale snapshot is never claimed healthy.
func (s *Server) handleFleetHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.fleetHealthSource == nil {
		w.Write([]byte(`{"stale":true,"fault":false,"pos":[],"error":"fleet-health source not wired"}`))
		return
	}
	status := s.fleetHealthSource()
	if status == nil {
		w.Write([]byte(`{"stale":true,"fault":false,"pos":[],"error":"no fleet-health snapshot yet"}`))
		return
	}
	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.Warn("encode fleet-health status", "err", err)
	}
}

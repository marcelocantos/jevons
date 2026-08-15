// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// SetPlanUsageSource registers the provider of the live subscription
// plan-usage picture served at GET /api/plan-usage (🎯T390).
func (s *Server) SetPlanUsageSource(f func() any) { s.planUsageSource = f }

// handlePlanUsage serves how much of each backend's subscription allowance is
// left and when it rolls over.
//
// Unwired reports disabled rather than an error, the same honesty shape as
// GET /api/cost and GET /api/capacity. The distinction the whole target turns
// on lives one level down, in the payload: a backend that publishes nothing is
// an explicit "unavailable" with a reason, never a blank or a zero.
func (s *Server) handlePlanUsage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.planUsageSource == nil {
		w.Write([]byte(`{"disabled":true,"error":"plan usage not enabled"}`))
		return
	}
	snap := s.planUsageSource()
	if snap == nil {
		w.Write([]byte(`{"disabled":true,"error":"no plan usage snapshot yet"}`))
		return
	}
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		slog.Warn("encode plan usage snapshot", "err", err)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// planUsageLongPoll is how long GET /api/plan-usage will wait for the first
// batch when the reader is still Pending. Sized just over the reader's default
// fetch timeout (20s) so one in-flight provider round can finish; on expiry the
// handler returns the pending snapshot and the cockpit retries.
const planUsageLongPoll = 30 * time.Second

// SetPlanUsageSource registers the provider of the live subscription
// plan-usage picture served at GET /api/plan-usage (🎯T390).
func (s *Server) SetPlanUsageSource(f func() any) { s.planUsageSource = f }

// SetPlanUsageWaitReady registers the long-poll wait for the first batch.
// When the current snapshot is Pending, handlePlanUsage blocks on this until
// the first successful fetch (or the request/long-poll deadline).
func (s *Server) SetPlanUsageWaitReady(f func(context.Context) error) {
	s.planUsageWaitReady = f
}

// SetPlanSweep registers the hot/exhausted migrate-or-park actuator
// (🎯T390.1.5), served at POST /api/plan-usage/sweep.
func (s *Server) SetPlanSweep(f func() any) { s.planSweep = f }

// handlePlanUsage serves how much of each backend's subscription allowance is
// left and when it rolls over.
//
// Unwired reports disabled rather than an error, the same honesty shape as
// GET /api/cost and GET /api/capacity. The distinction the whole target turns
// on lives one level down, in the payload: a backend that publishes nothing is
// an explicit "unavailable" with a reason, never a blank or a zero.
//
// When the first batch has not landed yet, the request long-polls until it
// does (or planUsageLongPoll / client cancel). Returning pending immediately
// forced the cockpit into a 5s busy-poll during daemon boot; holding the
// request lets one HTTP round-trip cover the wait.
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
	if planUsagePending(snap) && s.planUsageWaitReady != nil {
		ctx, cancel := context.WithTimeout(r.Context(), planUsageLongPoll)
		defer cancel()
		_ = s.planUsageWaitReady(ctx)
		if again := s.planUsageSource(); again != nil {
			snap = again
		}
	}
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		slog.Warn("encode plan usage snapshot", "err", err)
	}
}

func planUsagePending(snap any) bool {
	switch v := snap.(type) {
	case planusage.Snapshot:
		return v.Pending
	case *planusage.Snapshot:
		return v != nil && v.Pending
	default:
		return false
	}
}

func (s *Server) handlePlanUsageThresholds(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(planusage.DefaultThresholds()); err != nil {
		slog.Warn("encode plan usage thresholds", "err", err)
	}
}

func (s *Server) handlePlanUsageSweep(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.planSweep == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"plan sweep not enabled"}`))
		return
	}
	if err := json.NewEncoder(w).Encode(s.planSweep()); err != nil {
		slog.Warn("encode plan usage sweep", "err", err)
	}
}

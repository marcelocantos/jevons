// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
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

// SetPlanUsageRefresh registers the cockpit-reload poll (🎯T653).
func (s *Server) SetPlanUsageRefresh(f func(ctx context.Context) error) {
	s.planUsageRefresh = f
}

func wantsPlanUsageRefresh(r *http.Request) bool {
	if r == nil {
		return false
	}
	v := strings.TrimSpace(r.URL.Query().Get("refresh"))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "0", "false", "no":
		return false
	default:
		return true
	}
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
	if wantsPlanUsageRefresh(r) && s.planUsageRefresh != nil {
		ctx, cancel := context.WithTimeout(r.Context(), planUsageLongPoll)
		_ = s.planUsageRefresh(ctx)
		cancel()
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
	// 🎯T610: serve the daemon's verdict, not just the numbers behind it.
	// The cockpit used to classify for itself and drifted a whole model
	// behind; a band on the wire leaves it nothing to re-derive.
	if err := json.NewEncoder(w).Encode(planUsageWithBands(snap, time.Now())); err != nil {
		slog.Warn("encode plan usage snapshot", "err", err)
	}
}

// planUsageWithBands decorates whatever shape the source handed back. The
// source is an any-typed seam, so both the value and pointer cases are real;
// anything else is passed through untouched rather than dropped.
func planUsageWithBands(snap any, now time.Time) any {
	th := planusage.DefaultThresholds()
	switch v := snap.(type) {
	case planusage.Snapshot:
		return planusage.WithBands(v, now, th)
	case *planusage.Snapshot:
		if v == nil {
			return snap
		}
		return planusage.WithBands(*v, now, th)
	default:
		return snap
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

// planUsageSnapshotNow is the same payload GET /api/plan-usage encodes.
func (s *Server) planUsageSnapshotNow() any {
	if s == nil || s.planUsageSource == nil {
		return map[string]any{"disabled": true, "error": "plan usage not enabled"}
	}
	snap := s.planUsageSource()
	if snap == nil {
		return map[string]any{"disabled": true, "error": "no plan usage snapshot yet"}
	}
	return planUsageWithBands(snap, time.Now())
}

func (s *Server) writePlanUsage(ctx context.Context, conn muxConn) {
	s.muxWrite(ctx, conn, planUsageChannel, "frame", s.planUsageSnapshotNow())
}

// kickPlanUsageRefresh starts a forced producer poll so a mux open
// (cockpit reload) is not the 5-minute cache (🎯T653). The first frame
// already went out; FanPlanUsage follows OnUpdate.
func (s *Server) kickPlanUsageRefresh() {
	if s == nil || s.planUsageRefresh == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), planUsageLongPoll)
		defer cancel()
		_ = s.planUsageRefresh(ctx)
	}()
}

func (s *Server) FanPlanUsage() {
	if s == nil || s.mux == nil {
		return
	}
	payload, err := encodeMux(planUsageChannel, "frame", s.planUsageSnapshotNow())
	if err != nil {
		return
	}
	s.mux.mu.Lock()
	defer s.mux.mu.Unlock()
	for sess := range s.mux.conns {
		if sess.watchingPlanUsage() {
			sess.enqueue(payload)
		}
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

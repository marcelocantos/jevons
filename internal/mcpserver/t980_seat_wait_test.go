// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T980: a spawn refused because no plan can take a seat leaves its target
// waiting with the refusal's reason, served for the frontier play button,
// and the wait clears when the owner stops it.
func TestT980RefusedSpawnLeavesTheTargetWaiting(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, nil, nil)
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	s.SetRegistry(reg)
	now := time.Now()
	hot := func(p string) planusage.Backend {
		used, rem := 100.0, 0.0
		resets := now.Add(48 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Backend{Provider: p, Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	}
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{hot("claude"), hot("codex")}}
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "jv-t980-probe", "workdir": dir, "purpose": "work", "parent": "jevons-po", "target_id": "T980"}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError || !strings.Contains(toolText(res), "plan dest empty") {
		t.Fatalf("spawn on all-red plans = %q, want a plan dest empty refusal", toolText(res))
	}
	wait, ok := s.SeatWaits()["T980"]
	if !ok || !strings.Contains(wait.Reason, "claude") {
		t.Fatalf("seat wait = %+v %v, want T980 waiting with each plan's reason", wait, ok)
	}

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/seat-waits", nil))
	var served map[string]SeatWait
	if err := json.Unmarshal(rec.Body.Bytes(), &served); err != nil || served["T980"].Reason == "" {
		t.Fatalf("GET /api/seat-waits = %s, %v", rec.Body.String(), err)
	}

	s.ClearSeatWait("🎯T980")
	if _, ok := s.SeatWaits()["T980"]; ok {
		t.Fatal("a stopped request is still waiting")
	}
}

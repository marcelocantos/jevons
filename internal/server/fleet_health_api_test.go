// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 🎯T713: GET /api/fleet-health is the supervisor surface. Unwired reports
// stale rather than inventing healthy; a wired source is returned as JSON.

func TestT713FleetHealthUnwiredIsStaleNotHealthy(t *testing.T) {
	s := New("test", t.TempDir())
	rec := httptest.NewRecorder()
	s.handleFleetHealth(rec, httptest.NewRequest(http.MethodGet, "/api/fleet-health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["stale"] != true {
		t.Fatalf("unwired must be stale: %v", body)
	}
	if body["fault"] == true {
		t.Fatalf("unwired must not claim fault: %v", body)
	}
}

func TestT713FleetHealthWiredReturnsSource(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetFleetHealthSource(func() any {
		return map[string]any{
			"computed_at": "2026-09-21T00:34:00Z",
			"stale":       false,
			"fault":       true,
			"pos": []map[string]any{{
				"name":    "jevons-po",
				"verdict": "stalled",
				"reason":  "idle_on_ready_leaves",
				"fault":   true,
			}},
		}
	})
	rec := httptest.NewRecorder()
	s.handleFleetHealth(rec, httptest.NewRequest(http.MethodGet, "/api/fleet-health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["fault"] != true {
		t.Fatalf("want fault=true: %v", body)
	}
	if body["stale"] != false {
		t.Fatalf("want stale=false: %v", body)
	}
	pos, ok := body["pos"].([]any)
	if !ok || len(pos) != 1 {
		t.Fatalf("pos=%v", body["pos"])
	}
}

func TestT713FleetHealthRouteRegistered(t *testing.T) {
	s := New("test", t.TempDir())
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/fleet-health", nil))
	if rec.Code == http.StatusNotFound {
		t.Fatal("GET /api/fleet-health must be registered (was 404)")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// A live seat that has never streamed a turn has no turn phase. When the
// broker reports nothing in flight it is idle, not "unknown" (🎯T1276 J3:
// a freshly started codex overseer read "unknown" until its first turn).
func TestT1276APIAgentsFreshSeatWithNoTurnIsIdle(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "jevons"
	if err := reg.Register(claudia.AgentDef{Name: name, SessionID: "session", WorkDir: t.TempDir(), Provider: "openai-codex", Materialized: true}); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	a := seatstate.New(seatstate.Args{})
	s.SetSeats(a)
	s.SetRegistry(reg)
	hub := NewAgentProgressHub()
	hub.Observe(name, claudia.Event{Type: "progress", ProgressType: "idle", Text: "idle"})
	s.agentProgress = hub
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	get := func() agentInfo {
		t.Helper()
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/agents", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d: %s", rr.Code, rr.Body.String())
		}
		var rows []agentInfo
		if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("rows=%+v", rows)
		}
		return rows[0]
	}

	a.Observe(seatstate.Observation{Name: name, SessionID: "session", Alive: seatstate.Yes, InFlight: seatstate.No, Source: "broker.test", At: time.Now(), QueueDepth: seatstate.QueueUnknown})
	if row := get(); row.Phase != "idle" || row.PhaseHealth != "" {
		t.Fatalf("fresh seat with no turn in flight: phase=%q health=%q, want idle", row.Phase, row.PhaseHealth)
	}

}

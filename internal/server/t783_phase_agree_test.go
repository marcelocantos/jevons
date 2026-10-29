// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// The broker's turn-in-flight report is what the send queue consults. An idle
// transcript sample must not paint a working seat idle in the owner fleet tree.
func TestT783APIAgentsBrokerFlightOutranksIdlePhase(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "worker"
	if err := reg.Register(claudia.AgentDef{Name: name, SessionID: "session", WorkDir: t.TempDir(), Provider: "claude", Materialized: true}); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	a := seatstate.New(seatstate.Args{})
	s.SetSeats(a)
	s.SetRegistry(reg)
	a.Observe(seatstate.Observation{Name: name, SessionID: "session", Alive: seatstate.Yes, InFlight: seatstate.Yes, Phase: turnev.PhaseIdle, Source: "broker.test", At: time.Now(), QueueDepth: seatstate.QueueUnknown})
	hub := NewAgentProgressHub()
	hub.Observe(name, claudia.Event{Type: "progress", ProgressType: "idle", Text: "idle"})
	s.agentProgress = hub
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
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
	if rows[0].Phase != "working" || !strings.Contains(rows[0].PhaseHealth, "disagrees with broker turn in flight") || !strings.Contains(rows[0].PanelLine, "fleet health:") {
		t.Fatalf("broker flight/idle disagreement not surfaced: %+v", rows[0])
	}
}

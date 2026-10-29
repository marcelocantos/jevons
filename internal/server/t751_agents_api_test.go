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

	"github.com/marcelocantos/jevons/internal/seatliveness"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T751 — GET /api/agents carries process_liveness=dead without send.

func TestT751APIAgentsDeadSeatWithoutSend(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	const name = "jevons-po"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s1", Provider: "claude", Materialized: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	a := seatstate.New(seatstate.Args{})
	s.SetSeats(a)
	s.SetRegistry(reg)
	a.Observe(seatstate.Observation{
		Name: name, SessionID: "s1", Alive: seatstate.Yes, Phase: turnev.PhaseIdle,
		QueueDepth: seatstate.QueueUnknown, Source: "test.hub", At: time.Now(),
	})
	a.Observe(seatstate.Observation{
		Name: name, SessionID: "s1", Alive: seatstate.No,
		QueueDepth: seatstate.QueueUnknown, Source: "test.process", At: time.Now(),
	})
	hub := NewAgentProgressHub()
	hub.Observe(name, claudia.Event{Type: "progress", ProgressType: "idle", Text: "idle"})
	s.agentProgress = hub

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var agents []agentInfo
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("agents=%+v", agents)
	}
	row := agents[0]
	if row.ProcessLiveness != seatliveness.ServedDead {
		t.Fatalf("process_liveness=%q want dead", row.ProcessLiveness)
	}
	if row.Running {
		t.Fatalf("running=true on dead seat: %+v", row)
	}
	if row.Status != "stopped" {
		t.Fatalf("status=%q want stopped", row.Status)
	}
	if row.Phase == "idle" {
		t.Fatalf("stale hub left phase=idle on dead seat: %+v", row)
	}
	if row.TranscriptActivityReason != "provider process not running" {
		t.Fatalf("transcript_activity_reason=%q", row.TranscriptActivityReason)
	}
}

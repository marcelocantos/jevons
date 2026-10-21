// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/seatreg"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/server"
	"github.com/marcelocantos/jevons/internal/spool"
)

// 🎯T1078: a fresh daemon boots a brand-new overseer — no row before this
// boot, no turn, nothing materialized, nothing in the spool — and the broker
// reports it alive. The 30-second seat check can run before the isolate's
// readiness probe reads /api/agents (the race the probe usually won). The
// check had nothing that said the seat had a conversation, so it recorded
// phase_unknown, which /api/agents serves ahead of the alive → running
// fallback, and the probe, which accepts only running, gave up. A live
// overseer that has not been addressed yet is running and idle: it has no
// brief to miss. The test drives the production pieces in the race order:
// boot registration, the shared seat authority, the observation pass, then
// the HTTP feed.
func TestT1078FreshOverseerReadsRunningWhenTheSeatCheckWinsTheRace(t *testing.T) {
	const name = "jevons"
	t.Setenv("HOME", t.TempDir())
	state := t.TempDir()
	dir := filepath.Join(state, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(spool.DirEnv, filepath.Join(state, "spool")) // a fresh isolate: nothing spooled yet

	registry, err := seatreg.New(seatreg.Path(state))
	if err != nil {
		t.Fatal(err)
	}
	def, err := registerOverseerSeat(registry, name, dir, "", claudia.ProviderGrok)
	if err != nil {
		t.Fatal(err)
	}
	if def.Materialized {
		t.Fatal("fixture invalid: a brand-new overseer is not materialized")
	}

	// One authority for the daemon, as main wires it (🎯T766.2).
	seats := seatstate.New(seatstate.Args{})
	mcpSrv := mcpserver.New(t.TempDir(), nil, nil)
	mcpSrv.SetSeats(seats)
	mcpSrv.SetSendQueueDir(state)
	mcpSrv.SetRegistry(registry)
	httpSrv := server.New("test", state)
	httpSrv.SetSeats(seats)
	httpSrv.SetOverseerName(name)
	httpSrv.SetRegistry(registry)

	// The broker launched the seat and answers for it: alive, no turn.
	seats.FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: def.SessionID, Provider: string(def.Provider), Alive: true, Known: true,
	}, time.Now())

	// The seat check runs first.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mcpSrv.StartSeatObservations(ctx)
	deadline := time.Now().Add(10 * time.Second)
	var st seatstate.State
	for time.Now().Before(deadline) {
		st, _ = seats.Get(name)
		if st.Status != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Status == "" {
		t.Fatal("the seat check recorded no status for a live overseer")
	}

	// Then the readiness probe reads the feed.
	mux := http.NewServeMux()
	httpSrv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rows []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Name == name {
			if row.Status != "running" {
				t.Fatalf("fresh overseer served status %q after the seat check (recorded %q), want running", row.Status, st.Status)
			}
			return
		}
	}
	t.Fatalf("overseer missing from /api/agents: %+v", rows)
}

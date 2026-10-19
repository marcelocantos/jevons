// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/seatreg"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/spool"
)

// 🎯T1077: a restarted daemon re-registers an overseer that already holds a
// conversation on the sidecar. The state is the one gate 4c638044 left
// behind: the previous boot stored the plan id (grok) and never promoted
// Materialized, the spool holds the seat's turn, the broker kept the seat
// alive, and this process has seen no turn of its own. The production boot
// registration and the production observation pass must together read
// that seat as running — not phase_unknown, which /api/agents serves ahead
// of the live answer and which the isolate's readiness never accepted.
func TestT1077RestartedOverseerReadsRunning(t *testing.T) {
	const name = "jevons"
	const session = "c8a88bf9-3bf6-4cf5-a238-462caf6053a6"
	t.Setenv("HOME", t.TempDir())
	state := t.TempDir()
	dir := filepath.Join(state, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	spoolDir := filepath.Join(state, "spool")
	t.Setenv(spool.DirEnv, spoolDir)
	if err := os.MkdirAll(spoolDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// What the sidecar wrote for the seat's one completed turn.
	day := filepath.Join(spoolDir, "events-"+time.Now().UTC().Format(time.DateOnly)+".log")
	line := `{"ts":"` + time.Now().UTC().Format(time.RFC3339Nano) + `","seat":"` + name +
		`","session_id":"` + session + `","type":"turn_end"}` + "\n"
	if err := os.WriteFile(day, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	// The previous boot's row.
	prior, err := seatreg.New(seatreg.Path(state))
	if err != nil {
		t.Fatal(err)
	}
	if err := prior.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: session, Provider: claudia.ProviderGrok,
		AutoStart: true, Purpose: claudia.PurposeOverseer,
	}); err != nil {
		t.Fatal(err)
	}

	// The restart: a fresh registry over the same file, then the boot step.
	registry, err := seatreg.New(seatreg.Path(state))
	if err != nil {
		t.Fatal(err)
	}
	def, err := registerOverseerSeat(registry, name, dir, "", claudia.ProviderGrok)
	if err != nil {
		t.Fatal(err)
	}
	if def.SessionID != session {
		t.Fatalf("boot registration changed the session: %s, want %s", def.SessionID, session)
	}
	if got := registry.Def(name).Provider; got != "xai-oauth" {
		t.Errorf("overseer row provider = %q, want the sidecar seat id xai-oauth", got)
	}

	srv := mcpserver.New(t.TempDir(), nil, nil)
	srv.SetSendQueueDir(state)
	srv.SetRegistry(registry)
	// The broker answers for the seat it kept alive across the restart.
	srv.Seats().FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: session, Provider: "xai-oauth", Alive: true, Known: true,
	}, time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.StartSeatObservations(ctx)

	deadline := time.Now().Add(10 * time.Second)
	var st seatstate.State
	for time.Now().Before(deadline) {
		st, _ = srv.Seats().Get(name)
		if st.Status != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Status != "running" {
		t.Fatalf("restarted overseer with a spooled conversation reads status %q, want running", st.Status)
	}
}

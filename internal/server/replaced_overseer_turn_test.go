// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/seatstate"
)

// replacedOverseerServer has a live overseer seat in the registry and a
// notify queue whose open turn belongs to old.
func replacedOverseerServer(t *testing.T) (s *Server, live *claudia.Agent, sent *[]string) {
	t.Helper()
	const name = "jevons"
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "gen-replacement", Provider: claudia.ProviderGrok,
		Purpose: claudia.PurposeOverseer, Materialized: true,
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(context.Context, claudia.Config) (*claudia.Agent, error) {
			return claudia.NewStubAgent(func(string) error { return nil }), nil
		},
	})
	live, err = reg.Launch(name)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(func() { reg.Stop(name) })
	s = &Server{}
	s.SetOverseerName(name)
	s.SetRegistry(reg)
	// The replacement is alive and not in a turn.
	s.seats.Load().FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: "gen-replacement", Alive: true, Known: true,
	}, time.Now())
	sent = &[]string{}
	s.overseerSendSeam = func(text, _ string) error {
		*sent = append(*sent, text)
		return nil
	}
	return s, live, sent
}

// A plan-policy park stops the overseer mid-turn and the recovery sweep
// re-mints it. The converge loop attaches the new process; the open turn was
// the old process's and will never end. The queue must drain to the new seat.
func TestReattachingAReplacementOverseerSettlesTheOrphanedTurn(t *testing.T) {
	s, _, sent := replacedOverseerServer(t)
	s.mu.Lock()
	s.proc = claudia.NewStubAgent(func(string) error { return nil }) // the parked process
	s.waiting = true
	s.overseerOwnerTurn = true
	s.notifyQueue = []string{userTurnPrefix + "Reply with exactly: react-token"}
	s.mu.Unlock()

	if err := s.cockpitAttach(&cockpitState{}); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 1 || (*sent)[0] != userTurnPrefix+"Reply with exactly: react-token" {
		t.Fatalf("queued owner turn after re-attach = %v, want it handed to the new seat", *sent)
	}
}

// Re-attaching the same process is not a replacement: its open turn may
// still end, so the queue keeps waiting for it.
func TestReattachingTheSameOverseerKeepsItsOpenTurn(t *testing.T) {
	s, live, sent := replacedOverseerServer(t)
	s.mu.Lock()
	s.proc = live
	s.waiting = true
	s.notifyQueue = []string{"[worker] fleet note"}
	s.mu.Unlock()

	if err := s.cockpitAttach(&cockpitState{}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	waiting := s.waiting
	s.mu.Unlock()
	if len(*sent) != 0 || !waiting {
		t.Fatalf("same-process re-attach: sent=%v waiting=%v, want the open turn kept", *sent, waiting)
	}
}

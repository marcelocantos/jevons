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

// deadOverseerNoDownReason is registered but not alive and has no converge
// down-reason yet — the T775 residue window before the first tick.
func deadOverseerNoDownReason(t *testing.T, t0 time.Time) *Server {
	t.Helper()
	const name = "jevons"
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "dead", Provider: claudia.ProviderGrok,
		Purpose: claudia.PurposeOverseer, Materialized: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetOverseerName(name)
	s.SetRegistry(reg)
	s.seats.Load().FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: "dead", Alive: false, Known: true,
	}, t0)
	s.overseerPagingEpoch = t0
	return s
}

func liveOverseerOnRegistry(t *testing.T, s *Server) {
	t.Helper()
	s.mu.RLock()
	reg := s.registry
	name := s.overseerName
	s.mu.RUnlock()
	if reg == nil {
		t.Fatal("registry")
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(context.Context, claudia.Config) (*claudia.Agent, error) {
			return claudia.NewStubAgent(func(string) error { return nil }), nil
		},
	})
	agent, err := reg.Launch(name)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(func() { reg.Stop(name) })
	s.seats.Load().FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: agent.SessionID(), Alive: true, Known: true,
	}, time.Now())
}

// 🎯T807: never-ticked overseer down still pages after the threshold, once.
func TestT807NeverTickedOverseerDownPagesAfterThreshold(t *testing.T) {
	t0 := time.Now()
	s := deadOverseerNoDownReason(t, t0)
	var pages []string
	s.SetOverseerPager(func(_, body, _ string, rec bool) {
		if rec {
			return
		}
		pages = append(pages, body)
	})

	s.reconcileOverseerPage(t0.Add(DefaultOverseerPageAfter - time.Second))
	if len(pages) != 0 {
		t.Fatalf("paged before threshold: %v", pages)
	}
	s.reconcileOverseerPage(t0.Add(DefaultOverseerPageAfter))
	s.reconcileOverseerPage(t0.Add(time.Hour))
	if len(pages) != 1 || pages[0] != "overseer process is not running" {
		t.Fatalf("want one liveness page, got %v", pages)
	}
}

// 🎯T807: a late overseer start clears the outage before paging fires.
func TestT807LateStartClearsBeforePage(t *testing.T) {
	t0 := time.Now()
	s := deadOverseerNoDownReason(t, t0)
	var pages []string
	s.SetOverseerPager(func(_, body, _ string, rec bool) {
		if rec {
			return
		}
		pages = append(pages, body)
	})

	liveOverseerOnRegistry(t, s)

	s.reconcileOverseerPage(t0.Add(DefaultOverseerPageAfter))
	if len(pages) != 0 {
		t.Fatalf("healthy overseer should not page: %v", pages)
	}
}

// 🎯T807: after recovery the next outage pages again (re-arm).
func TestT807RearmAfterCauseClears(t *testing.T) {
	t0 := time.Now()
	s := deadOverseerNoDownReason(t, t0)
	var pages []string
	var recovered int
	s.SetOverseerPager(func(_, body, _ string, rec bool) {
		if rec {
			recovered++
			return
		}
		pages = append(pages, body)
	})

	s.reconcileOverseerPage(t0.Add(DefaultOverseerPageAfter))
	if len(pages) != 1 {
		t.Fatalf("first outage: %v", pages)
	}

	liveOverseerOnRegistry(t, s)
	s.reconcileOverseerPage(t0.Add(7 * time.Minute))
	if recovered != 1 {
		t.Fatalf("want recovery page, got %d", recovered)
	}

	s.mu.RLock()
	reg := s.registry
	name := s.overseerName
	s.mu.RUnlock()
	reg.Stop(name)
	s.seats.Load().FromClaudia(seatstate.SeatReport{
		Name: name, SessionID: "dead-again", Alive: false, Known: true,
	}, t0.Add(8*time.Minute))
	s.overseerPagingEpoch = t0.Add(8 * time.Minute)
	s.overseerDownSince = time.Time{}
	s.overseerPaged = false
	s.reconcileOverseerPage(t0.Add(8*time.Minute + DefaultOverseerPageAfter))
	if len(pages) != 2 {
		t.Fatalf("second outage should page again: %v", pages)
	}
}

// 🎯T807: the paging loop runs without waiting for EnsureOverseer.
func TestT807PageLoopIndependentOfConvergeTick(t *testing.T) {
	t0 := time.Now()
	s := deadOverseerNoDownReason(t, t0)
	s.overseerPagingEpoch = t0
	var pages []string
	s.SetOverseerPager(func(_, body, _ string, _ bool) {
		pages = append(pages, body)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.startOverseerPageLoop(ctx, 10*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for len(pages) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if len(pages) != 0 {
		t.Fatalf("page loop should not fire before %s, got %v", DefaultOverseerPageAfter, pages)
	}

	s.mu.Lock()
	s.overseerPagingEpoch = time.Now().Add(-DefaultOverseerPageAfter - time.Second)
	s.overseerDownSince = time.Time{}
	s.mu.Unlock()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	s.startOverseerPageLoop(ctx2, 10*time.Millisecond)

	deadline = time.Now().Add(2 * time.Second)
	for len(pages) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel2()
	if len(pages) != 1 {
		t.Fatalf("page loop should fire after threshold without converge tick, got %v", pages)
	}
}

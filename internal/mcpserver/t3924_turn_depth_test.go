// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"sync"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/turndepth"
)

// 🎯T392.4 hermetic: drive a turn past the ceiling through the shipped
// observeTurnDepth path. The daemon records the ask; it does not
// interrupt unless the fallback is armed.

func t3924Tool(n int) []claudia.Event {
	evs := make([]claudia.Event, 0, n+1)
	for i := 0; i < n; i++ {
		evs = append(evs, claudia.Event{Type: "progress", ProgressType: progressToolUse})
	}
	return evs
}

func TestT3924ObserveAsksAtTheCeiling(t *testing.T) {
	s := &Server{}
	s.SetTurnDepthPolicy(turndepth.Policy{Ceiling: turndepth.MinCeiling, Grace: -1})
	for _, ev := range t3924Tool(turndepth.MinCeiling - 1) {
		s.observeTurnDepth("jv-t392.4", ev)
	}
	if got := s.turnDepthStateCount(t, "jv-t392.4"); got != turndepth.MinCeiling-1 {
		t.Fatalf("depth below ceiling = %d", got)
	}
	s.observeTurnDepth("jv-t392.4", claudia.Event{Type: "progress", ProgressType: progressToolUse})
	_, pol := s.turnDepthState()
	st := s.turnDepthSnapshot(t, "jv-t392.4")
	if !st.Requested {
		t.Fatal("ceiling reached but checkpoint was not recorded")
	}
	if st.Interrupted {
		t.Fatal("disarmed fallback must not interrupt")
	}
	if pol.InterruptEnabled {
		t.Fatal("default/test policy here must keep interrupt disarmed")
	}
}

func TestT3924ArmedFallbackInterruptsOncePastGrace(t *testing.T) {
	s := &Server{}
	s.SetTurnDepthPolicy(turndepth.Policy{
		Ceiling: turndepth.MinCeiling, Grace: -1, InterruptEnabled: true,
	})
	var hits []string
	var mu sync.Mutex
	s.SetTurnDepthInterrupter(func(name string) error {
		mu.Lock()
		hits = append(hits, name)
		mu.Unlock()
		return nil
	})
	for _, ev := range t3924Tool(turndepth.MinCeiling) {
		s.observeTurnDepth("jv-t392.4", ev)
	}
	// The ceiling call asks; the next call, with grace 0, cuts.
	s.observeTurnDepth("jv-t392.4", claudia.Event{Type: "progress", ProgressType: progressToolUse})
	mu.Lock()
	defer mu.Unlock()
	if len(hits) != 1 || hits[0] != "jv-t392.4" {
		t.Fatalf("interrupt hits = %v; want one cut of jv-t392.4", hits)
	}
}

func TestT3924TerminalStopClearsTheTurn(t *testing.T) {
	s := &Server{}
	s.SetTurnDepthPolicy(turndepth.Policy{Ceiling: turndepth.MinCeiling})
	for _, ev := range t3924Tool(2) {
		s.observeTurnDepth("jv-t392.4", ev)
	}
	s.observeTurnDepth("jv-t392.4", claudia.Event{Type: "assistant", StopReason: "end_turn"})
	if got := s.turnDepthStateCount(t, "jv-t392.4"); got != 0 {
		t.Fatalf("depth after end_turn = %d; want 0 so the next turn starts clean", got)
	}
}

func (s *Server) turnDepthStateCount(t *testing.T, name string) int {
	t.Helper()
	c, _ := s.turnDepthState()
	return c.Depth(name)
}

func (s *Server) turnDepthSnapshot(t *testing.T, name string) turndepth.State {
	t.Helper()
	c, _ := s.turnDepthState()
	return c.State(name)
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/seatstate"
)

// SetSeats hands this server the daemon's one seat-state authority
// (🎯T766.2). main builds it once and gives the same instance to the MCP
// server and the fleet adapter, so the cockpit, the sweeps and the fleet see
// one answer about a seat instead of each deriving its own.
func (s *Server) SetSeats(a *seatstate.Authority) {
	if s == nil {
		return
	}
	s.seats.Store(a)
	s.mu.Lock()
	s.observeOwnerQueueLocked()
	s.mu.Unlock()
}

// seatInFlight tests for a positively observed running turn.
func (s *Server) seatInFlight(name string, _ *claudia.Agent) bool {
	return s.seatState(name).InFlight == seatstate.Yes
}

func (s *Server) seatState(name string) seatstate.State {
	if a := s.seats.Load(); a != nil {
		st, _ := a.Get(name)
		return st
	}
	return seatstate.State{Name: name, QueueDepth: seatstate.QueueUnknown, OwnerQueueDepth: seatstate.QueueUnknown}
}

// overseerSeatName is the registry name the overseer's process belongs to.
func (s *Server) overseerSeatName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.overseerName
}

// observeOwnerQueueLocked feeds the queue at construction/mutation boundaries.
// It is an in-memory queue: every mutation runs under mu, so its count remains
// valid until the next mutation (unlike sampled provider condition).
func (s *Server) observeOwnerQueueLocked() {
	if a := s.seats.Load(); a != nil {
		depth := len(s.notifyQueue)
		a.Observe(seatstate.Observation{Name: s.overseerName, OwnerQueueDepth: &depth,
			QueueDepth: seatstate.QueueUnknown, Source: "owner.queue"})
	}
}

func (s *Server) noteOverseerProgressLocked() {
	s.overseerLastProgress = time.Now()
	if a := s.seats.Load(); a != nil {
		a.Observe(seatstate.Observation{Name: s.overseerName, LastActivity: s.overseerLastProgress,
			QueueDepth: seatstate.QueueUnknown, Source: "owner.activity"})
	}
}

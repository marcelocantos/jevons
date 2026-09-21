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
}

// seatInFlight is internal/server's one reading of whether a turn is running
// on a seat — census derivation 5, of which this package held four copies.
//
// It asks claudia's process handle, which is the party that knows, and folds
// the answer into the shared authority so every other reader sees what the
// cockpit saw. A nil handle is not recorded at all: not having a handle is
// ignorance, and ignorance must not be written down as a fact.
func (s *Server) seatInFlight(name string, proc *claudia.Agent) bool {
	if proc == nil {
		return false
	}
	alive := proc.Alive()
	inFlight := alive && proc.PromptInFlight()
	if a := s.seats.Load(); a != nil && name != "" {
		a.FromClaudia(seatstate.SeatReport{
			Name: name, Alive: alive, PromptInFlight: inFlight, Known: true,
		}, time.Now())
	}
	return inFlight
}

// overseerSeatName is the registry name the overseer's process belongs to.
func (s *Server) overseerSeatName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.overseerName
}

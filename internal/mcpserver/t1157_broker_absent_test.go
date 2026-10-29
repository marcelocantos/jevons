// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T1157: a successful broker listing is the broker's whole seat set. A
// registered seat missing from it reads not-alive instead of decaying to
// Unknown (the phase_unknown rows of 2026-10-22).
func TestT1157RegisteredSeatAbsentFromBrokerReadsNotAlive(t *testing.T) {
	s := &Server{registry: regWithTree(t)}
	broker := map[string]claudia.BrokerSeat{
		"jevons": {Name: "jevons", Alive: true, Owned: true},
	}
	s.observeBrokerSeats(broker)

	if got := s.seatState("jevons").Alive; got != seatstate.Yes {
		t.Errorf("listed seat Alive = %v, want Yes", got)
	}
	for _, name := range []string{"po", "worker", "peer"} {
		if got := s.seatState(name).Alive; got != seatstate.No {
			t.Errorf("%s absent from the broker listing: Alive = %v, want No", name, got)
		}
	}
}

// A failed or unavailable listing says nothing: not knowing stays Unknown.
func TestT1157NilBrokerListingObservesNothing(t *testing.T) {
	s := &Server{registry: regWithTree(t)}
	s.observeBrokerSeats(nil)
	if got := s.seatState("worker").Alive; got != seatstate.Unknown {
		t.Errorf("nil listing: Alive = %v, want Unknown", got)
	}
}

// A live local process wins the fold: a seat this daemon holds is not marked
// dead because the broker listing missed it.
func TestT1157LocalProcessOutranksBrokerAbsence(t *testing.T) {
	s := &Server{registry: regWithTree(t)}
	s.Seats().Observe(seatstate.Observation{Name: "worker", LocalAlive: seatstate.Yes, QueueDepth: seatstate.QueueUnknown, Source: "test.local"})
	s.observeBrokerSeats(map[string]claudia.BrokerSeat{})
	if got := s.seatState("worker").Alive; got != seatstate.Yes {
		t.Errorf("locally alive seat absent from the listing: Alive = %v, want Yes", got)
	}
}

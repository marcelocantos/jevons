// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T766.2: no handle is ignorance, and ignorance is never written down.
func TestT766SeatInFlightDoesNotRecordAMissingHandle(t *testing.T) {
	s := &Server{}
	a := seatstate.New(seatstate.Args{})
	s.SetSeats(a)
	if s.seatInFlight("jevons", nil) {
		t.Fatal("a nil handle reported a turn in flight")
	}
	if _, ok := a.Get("jevons"); ok {
		t.Fatal("a missing handle was recorded in the authority as a fact")
	}
}

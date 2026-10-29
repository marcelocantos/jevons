// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatliveness is the registry/API answer for whether a seat's
// provider process is up, without sending to the seat (🎯T751).
package seatliveness

import (
	"github.com/marcelocantos/jevons/internal/seatstate"
)

const (
	ServedLive    = "live"
	ServedDead    = "dead"
	ServedUnknown = "unknown"
)

// Served maps the seat authority's process reading to a stable API value.
func Served(st seatstate.State) string {
	switch st.Alive {
	case seatstate.Yes:
		return ServedLive
	case seatstate.No:
		return ServedDead
	default:
		return ServedUnknown
	}
}

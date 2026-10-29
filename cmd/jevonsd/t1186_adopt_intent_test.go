// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// A held auto seat is re-adopted on restart only when its own intent does not
// keep it down. On 2026-10-22 forty workers parked minutes before a restart
// were re-adopted and read intent_fault.
func TestAdoptPausedAutoSeatsSkipsStoodDownSeats(t *testing.T) {
	snap := fleetintent.Snapshot{Agents: map[string]fleetintent.Record{
		"parked": {State: fleetintent.Parked},
		"reaped": {State: fleetintent.Reaped},
	}}
	allow := func(name string, c fleetintent.Control) fleetintent.Decision { return snap.Allow(name, c) }
	held := func(string) string { return "frontier_consume_disabled" }
	for name, want := range map[string]bool{"parked": false, "reaped": false, "live": true} {
		if got := adoptsHeldAutoSeat(name, true, held, allow); got != want {
			t.Errorf("adoptsHeldAutoSeat(%q) = %v, want %v", name, got, want)
		}
	}
	if adoptsHeldAutoSeat("live", false, held, allow) {
		t.Error("a seat that is not AutoStart is not this path's to adopt")
	}
	if adoptsHeldAutoSeat("live", true, func(string) string { return "" }, allow) {
		t.Error("an unheld seat reattaches through the ordinary path")
	}
}

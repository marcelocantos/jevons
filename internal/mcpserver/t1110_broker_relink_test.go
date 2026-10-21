// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T1110 acceptance 1 (hermetic half): a registered seat this host
// believes alive, whose broker grant is unowned after a simulated relink,
// is a candidate for re-own/stop — never silently left as plain running.
func TestUnownedWhileAliveSeats(t *testing.T) {
	alive := map[string]bool{
		"jv-work-1": true,
		"jv-work-2": true,
		"jevons-po": true,
		"jv-down":   false,
	}
	broker := map[string]seatstate.State{
		"jv-work-1": {BrokerOwned: seatstate.No},
		"jv-work-2": {BrokerOwned: seatstate.Yes},
		"jevons-po": {BrokerOwned: seatstate.No},
		"jv-down":   {BrokerOwned: seatstate.No},
	}
	names := []string{"jv-work-1", "jv-work-2", "jevons-po", "jv-down"}
	got := unownedWhileAliveSeats(names, func(n string) bool { return alive[n] }, broker, fleetintent.Snapshot{})

	want := map[string]bool{"jv-work-1": true, "jevons-po": true}
	if len(got) != len(want) {
		t.Fatalf("unownedWhileAliveSeats() = %v, want keys of %v", got, want)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected candidate %q", name)
		}
	}
	// jv-work-2 is owned (not a candidate); jv-down is not locally alive
	// (the classic 🎯T925 reattach sweep owns that shape, not this one).
	for _, name := range got {
		if name == "jv-work-2" || name == "jv-down" {
			t.Errorf("got %q, want it excluded", name)
		}
	}
}

// A seat whose fleet intent has stood it down is left to the 🎯T984
// stand-down sweep: this one must not touch it even if it is locally
// alive and broker-unowned.
func TestUnownedWhileAliveSeats_StoodDownExcluded(t *testing.T) {
	alive := map[string]bool{"jv-parked": true}
	broker := map[string]seatstate.State{"jv-parked": {BrokerOwned: seatstate.No}}
	intent := fleetintent.Snapshot{Agents: map[string]fleetintent.Record{
		"jv-parked": {State: fleetintent.Parked},
	}}

	got := unownedWhileAliveSeats([]string{"jv-parked"}, func(n string) bool { return alive[n] }, broker, intent)
	if len(got) != 0 {
		t.Fatalf("unownedWhileAliveSeats() = %v, want empty for a stood-down seat", got)
	}
}

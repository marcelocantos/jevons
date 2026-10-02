// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T983: a restart reattaches no seat whose own intent keeps it down. On
// 2026-10-02 every daemon boot had started three workers jevons-po parked on
// 2026-09-30, two of them finished.
func TestT983RestartReattachSkipsParkedSeats(t *testing.T) {
	snap := fleetintent.Snapshot{
		Fleet: fleetintent.Record{State: fleetintent.BlockedProvider},
		Agents: map[string]fleetintent.Record{
			"parked":  {State: fleetintent.Parked},
			"blocked": {State: fleetintent.BlockedOwner},
			"reaped":  {State: fleetintent.Reaped},
		},
	}
	include := fleetReattachInclude(
		func(name string) bool { return name == "jevons" },
		func(name string, c fleetintent.Control) fleetintent.Decision { return snap.Allow(name, c) },
	)
	for name, want := range map[string]bool{
		"jevons":  false, // the overseer reattaches first, on its own
		"parked":  false,
		"blocked": true, // waiting on the owner: the answer needs a live seat
		"reaped":  false,
		// A fleet-wide provider block does not keep a working seat from its
		// daemon: the block is transient and the seat is still this one's.
		"worker": true,
	} {
		if got := include(name); got != want {
			t.Errorf("include(%q) = %v, want %v", name, got, want)
		}
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/mcpserver"
)

// 🎯T769: after a development-daemon restart, AutoStart fleet seats (including
// the default PO) must be candidates for boot reattach — otherwise sendq
// correctly reports "no live process" forever (2026-09-21 jevons-po incident).
func TestT769RestartReattachIncludesAutoStartPO(t *testing.T) {
	allowAll := func(string, fleetintent.Control) fleetintent.Decision {
		return fleetintent.Decision{Allow: true}
	}
	overseer := func(name string) bool { return name == "jevons" }
	heldAuto := mcpserver.FrontierWorkerName("T703")
	include := fleetReattachInclude(overseer, allowAll, func(name string) string {
		if name == heldAuto {
			return "frontier pause (🎯T1067)"
		}
		return ""
	})
	for name, want := range map[string]bool{
		"jevons-po":      true,
		"jv-t769-worker": true,
		"jevons":         false,
		heldAuto:         false,
	} {
		if got := include(name); got != want {
			t.Errorf("include(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestT769RestartReattachSkipsStoodDownPO(t *testing.T) {
	snap := fleetintent.Snapshot{
		Agents: map[string]fleetintent.Record{
			"jevons-po": {State: fleetintent.Parked},
		},
	}
	allow := func(name string, c fleetintent.Control) fleetintent.Decision {
		return snap.Allow(name, c)
	}
	overseer := func(name string) bool { return name == "jevons" }
	include := fleetReattachInclude(overseer, allow, func(string) string { return "" })
	if include("jevons-po") {
		t.Fatal("stood-down PO must not reattach on restart (🎯T983)")
	}
}

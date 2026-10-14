// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/mcpserver"
)

// 🎯T1067: a restart under the frontier pause (frontier_consume.disabled)
// does not start auto-spawned workers. On 2026-10-12 one launched 57 Claude
// processes for -auto seats and the PO froze the daemon with SIGSTOP to stop
// them. Named workers and POs still reattach.
func TestT1067RestartReattachHoldsPausedAutoWorkers(t *testing.T) {
	allowAll := func(string, fleetintent.Control) fleetintent.Decision { return fleetintent.Decision{Allow: true} }
	overseer := func(name string) bool { return name == "jevons" }
	auto := mcpserver.FrontierWorkerName("T703")
	for _, paused := range []bool{true, false} {
		include := fleetReattachInclude(overseer, allowAll, func(name string) bool {
			return paused && mcpserver.IsAutoSpawnedWorkerName(name)
		})
		for name, want := range map[string]bool{
			auto:              !paused,
			"jv-t1066-worker": true,
			"jevons-po":       true,
			"jevons":          false,
		} {
			if got := include(name); got != want {
				t.Errorf("paused=%v: include(%q) = %v, want %v", paused, name, got, want)
			}
		}
	}
}

// The owner's fleet hold (fleet intent blocked_owner) must apply at boot: no
// -auto seat is launched by the restart reattach, and the adopt-only path
// covers it instead. Named workers, POs and an un-held fleet are unaffected.
func TestT1067BootHonoursOwnerFleetHold(t *testing.T) {
	overseer := func(name string) bool { return name == "jevons" }
	auto := mcpserver.FrontierWorkerName("T703")
	for _, fleet := range []fleetintent.State{fleetintent.BlockedOwner, fleetintent.Parked, fleetintent.BlockedProvider, fleetintent.Working, fleetintent.Unknown} {
		allow := func(name string, c fleetintent.Control) fleetintent.Decision {
			return fleetintent.Allows(fleet, fleetintent.Working, c)
		}
		held := fleetintent.Resolve(fleet) != fleetintent.Working
		pausedAuto := func(name string) bool { return bootHoldsAuto(name, false, allow) }
		include := fleetReattachInclude(overseer, allow, pausedAuto)
		for name, want := range map[string]bool{
			auto:              !held,
			"jv-t1066-worker": true,
			"jevons-po":       true,
			"jevons":          false,
		} {
			if got := include(name); got != want {
				t.Errorf("fleet=%q: include(%q) = %v, want %v", fleet, name, got, want)
			}
		}
		if got := pausedAuto(auto); got != held {
			t.Errorf("fleet=%q: bootHoldsAuto(auto) = %v, want %v (adopt-only path)", fleet, got, held)
		}
		if pausedAuto("jevons-po") {
			t.Errorf("fleet=%q: a named PO was held at boot", fleet)
		}
	}
	if !bootHoldsAuto(auto, true, nil) {
		t.Error("frontier pause alone no longer holds -auto seats")
	}
}

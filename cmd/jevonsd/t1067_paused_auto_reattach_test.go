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

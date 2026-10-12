// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T1067: under the frontier pause the reattach loop never relaunches an
// auto-spawned worker the broker lost; a named worker it lost still comes back.
func TestT1067PausedAutoWorkerIsNotRelaunched(t *testing.T) {
	auto := FrontierWorkerName("T703")
	for _, paused := range []bool{true, false} {
		s := &Server{}
		s.SetAutoSpawnPaused(paused)
		s.NoteDeadSeat(auto, fleet.ExitCauseBrokerLost, "broker restarted")
		s.NoteDeadSeat("jv-t1066-worker", fleet.ExitCauseBrokerLost, "broker restarted")
		if got := s.relaunchableLostSeat(auto); got != !paused {
			t.Errorf("paused=%v: relaunchable(%s) = %v, want %v", paused, auto, got, !paused)
		}
		if !s.relaunchableLostSeat("jv-t1066-worker") {
			t.Errorf("paused=%v: a named worker lost to the broker is not relaunched", paused)
		}
	}
}

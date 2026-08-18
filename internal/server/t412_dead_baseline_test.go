// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T412: the RHS baseline renders a dead-unmaterialized seat as dead — never
// as idle, which is the chrome that made six ghosts look like a resting fleet.
func TestT412BaselineSaysDeadNotIdle(t *testing.T) {
	phase, summary := statusBaseline(fleet.StatusDeadUnmaterialized)
	if summary == "" {
		t.Fatal("dead_unmaterialized produced no baseline: the RHS would keep the previous (running) chrome")
	}
	if summary == "idle" || summary == "running" {
		t.Fatalf("dead_unmaterialized baseline summary %q reads as alive", summary)
	}
	if phase != "idle" {
		t.Fatalf("dead_unmaterialized baseline phase %q want idle (no busy chrome for a corpse)", phase)
	}
}

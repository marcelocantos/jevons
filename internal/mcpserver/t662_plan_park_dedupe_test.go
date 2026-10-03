// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/seatstop"
)

// A second plan-policy park of an already-stopped seat must not mint a new
// seat_stop — that is what re-fired MASS STOP every sweep under depletion.
func TestAlreadyPlanParkedSkipsReNote(t *testing.T) {
	s := &Server{}
	reason := "plan policy parked: resolve: token-tied models"
	s.noteSeatStop("bullseye-po", seatstop.SourcePlanPolicy, reason, planPolicyActor, "")
	if !s.alreadyPlanParked("bullseye-po", reason) {
		t.Fatal("expected already plan-parked after first note")
	}
	// Different wording still counts while seat is down (prefix match).
	if !s.alreadyPlanParked("bullseye-po", "plan policy parked: other resolve detail") {
		t.Fatal("expected any plan-policy park while down to skip re-note")
	}
	if s.alreadyPlanParked("never-parked", reason) {
		t.Fatal("unknown seat must not look already parked")
	}
}

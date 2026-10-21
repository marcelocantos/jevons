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
	if !s.alreadyPlanParked("bullseye-po") {
		t.Fatal("expected already plan-parked after first note")
	}
	// A plan-policy stop whose reason does not use the old sentence still counts.
	s.noteSeatStop("worded", seatstop.SourcePlanPolicy, "claude weekly exhausted", planPolicyActor, "")
	if !s.alreadyPlanParked("worded") {
		t.Fatal("plan-policy source counts without the rendered sentence")
	}
	// The rendered sentence on another source is not a plan park.
	s.noteSeatStop("spoofed", seatstop.SourceExit, reason, planPolicyActor, "")
	if s.alreadyPlanParked("spoofed") {
		t.Fatal("rendered plan-policy sentence on another source counted as a plan park")
	}
	if s.alreadyPlanParked("never-parked") {
		t.Fatal("unknown seat must not look already parked")
	}
}

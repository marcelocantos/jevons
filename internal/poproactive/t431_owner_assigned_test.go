// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package poproactive

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/ownergate"
)

// 🎯T431: any leaf carrying owned_by is not ready work. The T370 shape is
// the load-bearing case — assigned to the owner so auto-spawn stops, with
// a reason that is NOT the 🎯T449 awaiting-verdict marker. Parking only on
// that marker would leave T370 ready and re-spawn it.

func TestT431OwnerAssignedWithoutVerdictMarkerParks(t *testing.T) {
	got := ClassifyLeaf(LeafObs{
		ID:            "T370",
		Name:          "keypress residual",
		OwnedBy:       ownergate.OwnerHandle,
		OwnedByReason: "Assigning so frontier-consume stops re-spawning workers onto it",
	})
	if got != LeafSkipAwaitingOwnerVerdict {
		t.Fatalf("ClassifyLeaf = %s, want skip_awaiting_owner_verdict (owner assignment is the park)", got)
	}
}

func TestT431CompanionUnassignedStillReady(t *testing.T) {
	if got := ClassifyLeaf(LeafObs{
		ID:   "T500",
		Name: "ordinary ready Build leaf",
	}); got != LeafReady {
		t.Fatalf("unassigned companion classified %s, want ready — the skip must not disable auto-spawn", got)
	}
}

func TestT431OwnedByOtherDriverAlsoParks(t *testing.T) {
	got := ClassifyLeaf(LeafObs{
		ID:            "T385",
		Name:          "driven elsewhere",
		OwnedBy:       "bullseye-po",
		OwnedByReason: "driving this from the bullseye side",
	})
	if got != LeafSkipOwnedByOther {
		t.Fatalf("ClassifyLeaf = %s, want skip_owned_by", got)
	}
}

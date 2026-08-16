// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package poproactive

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/ownergate"
)

// 🎯T449 clause 3, at the classifier. The fixture is 🎯T383's real shape: a
// leaf whose code landed at 8f25010 and whose sole residue is the owner
// hard-reloading the page and saying whether it still jumps.
//
// The two controls are what stop the fix being "park more broadly". Parking
// everything that mentions a commit would pass the first assertion and be
// worse than the bug — so an untouched leaf with the IDENTICAL status must
// still be ready, and a gate the owner has already answered must return to
// normal handling.

func t449AwaitingReason(t *testing.T) string {
	t.Helper()
	reason, err := ownergate.Record{
		Question:   "After a hard reload, does the second aside stay selected?",
		Evidence:   "landed at 8f25010; GATE node id=adf36ad8 GREEN",
		RecordedBy: "jevons-po",
	}.Reason()
	if err != nil {
		t.Fatal(err)
	}
	return reason
}

func TestT449ClassifyAwaitingOwnerVerdictParks(t *testing.T) {
	got := ClassifyLeaf(LeafObs{
		ID:            "T383",
		Name:          "Sticky fleet-tree selection",
		OwnedBy:       ownergate.OwnerHandle,
		OwnedByReason: t449AwaitingReason(t),
	})
	if got != LeafSkipAwaitingOwnerVerdict {
		t.Fatalf("ClassifyLeaf = %s, want skip_awaiting_owner_verdict", got)
	}
	if got.String() != "skip_awaiting_owner_verdict" {
		t.Fatalf("kind renders as %q", got.String())
	}
}

// Control 1: same status, same shape, no assignment — the sweep must still
// treat it as work nobody has started.
func TestT449ControlUntouchedLeafStillReady(t *testing.T) {
	if got := ClassifyLeaf(LeafObs{
		ID:   "T384",
		Name: "Sticky fleet-tree selection",
		// Prose that sounds finished, deliberately: nothing here infers the
		// state from how done the text reads.
		Context: "Implemented and landed at 8f25010; gates green; only the owner's hard reload is left.",
	}); got != LeafReady {
		t.Fatalf("untouched leaf classified %s, want ready — the fix must not park on prose", got)
	}
}

// Control 2: the owner has answered. The gate is spent, so the leaf goes back
// to normal handling — on reject that is how work resumes from the landed
// commit without waiting for the assignment to be unwound first.
func TestT449ControlAnsweredGateReturnsToNormalHandling(t *testing.T) {
	base := t449AwaitingReason(t)
	for _, v := range []ownergate.Verdict{ownergate.VerdictAccept, ownergate.VerdictReject} {
		obs := LeafObs{
			ID:            "T383",
			Name:          "Sticky fleet-tree selection",
			OwnedBy:       ownergate.OwnerHandle,
			OwnedByReason: base + " " + ownergate.FormatAnswer(v, "", "jevons-po",
				time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)),
		}
		if got := ClassifyLeaf(obs); got != LeafReady {
			t.Fatalf("%s: answered gate classified %s, want ready", v, got)
		}
	}
}

// A target driven by another agent is a different claim with a different park
// reason: nobody should conclude from it that work is finished.
func TestT449OwnedByAnotherDriverParksSeparately(t *testing.T) {
	got := ClassifyLeaf(LeafObs{
		ID:            "T385",
		Name:          "Ledger-side defect",
		OwnedBy:       "bullseye-po",
		OwnedByReason: "driving this from the bullseye side",
	})
	if got != LeafSkipOwnedByOther {
		t.Fatalf("ClassifyLeaf = %s, want skip_owned_by", got)
	}
}

// The owner's own override still wins — force-engage is how the owner says
// "work it anyway", and the gate is theirs to waive.
func TestT449ForceEngageOverridesTheGate(t *testing.T) {
	obs := LeafObs{
		ID:            "T383",
		OwnedBy:       ownergate.OwnerHandle,
		OwnedByReason: t449AwaitingReason(t),
		Tags:          []string{"force-engage"},
	}
	if got := ClassifyLeaf(obs); got != LeafReady {
		t.Fatalf("force-engage leaf classified %s, want ready", got)
	}
}

// The pass as a whole sleeps when the only leaves left are owner gates:
// otherwise the PO would kick forever against work that is finished.
func TestT449PassSleepsWhenOnlyOwnerGatesRemain(t *testing.T) {
	d := Classify([]LeafObs{
		{ID: "T383", OwnedBy: ownergate.OwnerHandle, OwnedByReason: t449AwaitingReason(t)},
		{ID: "T385", OwnedBy: "bullseye-po", OwnedByReason: "driven elsewhere"},
	})
	if d.Mode != Sleep {
		t.Fatalf("Mode = %s, want sleep (%+v)", d.Mode, d)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"strings"
	"testing"
)

// 🎯T449: the loader carries `owned_by` verbatim.
//
// The leaf must still BE a leaf. bullseye drops owned targets from its own
// frontier, and the tempting fix here was to do the same — but a leaf that
// vanishes is indistinguishable from one that was never ready, and the whole
// defect being fixed is a reader that cannot tell finished work from
// untouched work. So the loader carries the assignment and the consume
// classifier parks on it, loudly, with the recorded reason attached.
const t449OwnedByLedger = `
targets:
  T383:
    name: Sticky fleet-tree selection
    status: identified
    owned_by:
      owner: owner
      reason: >-
        BUILT, AWAITING OWNER VERDICT — recorded 2026-08-15 by jevons-po
        (🎯T449). Owner gate: does the second aside stay selected across a
        hard reload? Evidence: landed at 8f25010, GATE node id=adf36ad8.
  T384:
    name: Untouched leaf with the identical status
    status: identified
  T385:
    name: Parked with another driver
    status: identified
    owned_by:
      owner: bullseye-po
      reason: driving this from the bullseye side
  T386:
    name: Assignment written then emptied
    status: identified
    owned_by:
`

func TestT449FrontierLeavesCarryOwnedBy(t *testing.T) {
	leaves, err := FrontierLeaves([]byte(t449OwnedByLedger))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]FrontierLeaf{}
	for _, l := range leaves {
		byID[l.ID] = l
	}
	for _, id := range []string{"T383", "T384", "T385", "T386"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("%s dropped from the frontier: %+v", id, leaves)
		}
	}

	owned := byID["T383"]
	if owned.OwnedBy != "owner" {
		t.Fatalf("T383 OwnedBy = %q, want owner", owned.OwnedBy)
	}
	if !strings.Contains(owned.OwnedByReason, "BUILT, AWAITING OWNER VERDICT") ||
		!strings.Contains(owned.OwnedByReason, "8f25010") {
		t.Fatalf("T383 reason not carried verbatim: %q", owned.OwnedByReason)
	}

	if byID["T384"].OwnedBy != "" || byID["T384"].OwnedByReason != "" {
		t.Fatalf("untouched control acquired an assignment: %+v", byID["T384"])
	}
	if byID["T385"].OwnedBy != "bullseye-po" {
		t.Fatalf("T385 OwnedBy = %q", byID["T385"].OwnedBy)
	}
	// `owned_by:` with a null value is the shape an unassign leaves behind.
	// It must decode as no assignment, not as an error and not as an empty
	// owner that reads like a gate.
	if byID["T386"].OwnedBy != "" {
		t.Fatalf("null owned_by decoded as an assignment: %+v", byID["T386"])
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"strings"
	"testing"
)

// The ledger shape an achieved row has, including the multi-paragraph
// attestation and context that the 🎯T728 ceremony must read before a reopen
// clears them.
const t728Ledger = `targets:
  T711:
    name: PO seats must preempt
    status: achieved
    achieved: 2026-09-21
    attestation: |-
      Achieved by jevons-po on the hermetic clauses.

      ORACLE: GATE exit=0 GREEN id=45acfeb7.
    context: |-
      Owner report 2026-09-20.

      Achieved 2026-09-21: Achieved by jevons-po on the hermetic clauses.
  T383:
    name: An active row parked with the owner
    status: identified
    owned_by:
      owner: owner
      reason: BUILT, AWAITING OWNER VERDICT — landed at abcdef1.
  T2:
    name: A plain active row
    status: identified
`

func TestT728GateRowReadsAnAchievedRow(t *testing.T) {
	row, ok := LookupGateRow([]byte(t728Ledger), "🎯T711")
	if !ok {
		t.Fatal("achieved row not found — the ceremony would conclude the target is absent")
	}
	if !row.IsAchieved() {
		t.Fatalf("status=%q did not read as achieved", row.Status)
	}
	if row.Achieved != "2026-09-21" {
		t.Errorf("achieved=%q, want 2026-09-21", row.Achieved)
	}
	// The whole point of reading the row first: these two fields are what a
	// reopen destroys, so a reader that dropped either would lose them silently.
	if !strings.Contains(row.Attestation, "ORACLE: GATE exit=0 GREEN id=45acfeb7.") {
		t.Errorf("attestation truncated at a blank line: %q", row.Attestation)
	}
	if !strings.Contains(row.Context, "Achieved 2026-09-21: ") {
		t.Errorf("context lost the achieve audit: %q", row.Context)
	}
	if row.OwnedByOwner != "" {
		t.Errorf("invented an owner on an unassigned row: %q", row.OwnedByOwner)
	}
}

func TestT728GateRowReadsTheAssignmentReason(t *testing.T) {
	row, ok := LookupGateRow([]byte(t728Ledger), "T383")
	if !ok {
		t.Fatal("row not found")
	}
	if row.IsAchieved() {
		t.Error("an identified row read as achieved")
	}
	if row.OwnedByOwner != "owner" {
		t.Errorf("owner=%q, want owner", row.OwnedByOwner)
	}
	// LookupTargetOwner drops the reason, which is why this reader exists: the
	// answer half decides what accept means by reading it.
	if !strings.Contains(row.OwnedByReason, "AWAITING OWNER VERDICT") {
		t.Errorf("reason not carried: %q", row.OwnedByReason)
	}
}

// Unknown stays unknown — the residual every reader in this package takes. An
// absent row must not read as an achieved one, or the ceremony would reopen
// something it never looked at.
func TestT728GateRowUnknownStaysUnknown(t *testing.T) {
	for _, tc := range []struct{ name, ledger, id string }{
		{"absent id", t728Ledger, "T999"},
		{"empty id", t728Ledger, ""},
		{"not yaml", "%%% not a ledger", "T711"},
		{"no targets key", "other: 1\n", "T711"},
	} {
		row, ok := LookupGateRow([]byte(tc.ledger), tc.id)
		if ok {
			t.Errorf("%s: reported a row: %+v", tc.name, row)
		}
		if row.IsAchieved() {
			t.Errorf("%s: an unknown row read as achieved", tc.name)
		}
	}
}

// A row with no assignment block at all, and one whose `owned_by:` is null —
// the shape an unassign leaves behind — must both read as unassigned rather
// than erroring.
func TestT728GateRowToleratesNullAssignment(t *testing.T) {
	row, ok := LookupGateRow([]byte("targets:\n  T2:\n    status: identified\n    owned_by:\n"), "T2")
	if !ok {
		t.Fatal("row with a null owned_by was dropped")
	}
	if row.OwnedByOwner != "" {
		t.Errorf("owner=%q, want empty", row.OwnedByOwner)
	}
}

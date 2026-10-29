// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"strings"
)

// The 🎯T449 ceremony needs one thing the existing readers cannot answer:
// whether the row it is about to record a gate on is achieved, and if it is,
// what its closure said (🎯T728).
//
// Neither existing reader reaches it. LookupTargetOwner returns the owner but
// not the reason, and FrontierLeaves only walks active rows — an achieved
// target is not a frontier leaf, so a ceremony reading through it would see
// nothing at all and conclude the row was absent. Hence one more narrow view,
// in the style of assignmentDoc: a struct of its own rather than a field bolted
// onto a struct on the kickoff path, because several workers share this tree.

// GateRow is the narrow view of a single row the owner-gate ceremony reads:
// enough to tell an achieved row from an active one, and enough to carry an
// achieved row's closure across a reopen without losing it.
type GateRow struct {
	// ID is the row's id, without the 🎯 prefix.
	ID string
	// Status is the raw `status` string (identified, converging, achieved,
	// set_aside). Empty when the row carries none.
	Status string
	// Achieved is the `achieved` date (YYYY-MM-DD) on an achieved row. A
	// reopen clears it, which is why the ceremony reads it first.
	Achieved string
	// Attestation is the `attestation` text on an achieved row. A reopen
	// clears this too (🎯T64 status-scoped hygiene), so the ceremony must
	// make sure it survives elsewhere before reopening.
	Attestation string
	// Context is the row's `context` — where bullseye's own achieve and
	// reopen audit lines accumulate, and therefore where an attestation
	// dropped by a reopen can still be read back.
	Context string
	// OwnedByOwner and OwnedByReason are the assignment, when present.
	OwnedByOwner  string
	OwnedByReason string
}

// IsAchieved reports the achieved status by the ledger's own spelling.
func (r GateRow) IsAchieved() bool { return strings.TrimSpace(r.Status) == "achieved" }

// LookupGateRow reads one row out of a ledger YAML document. ok is false when
// the document does not parse or does not carry that id — unknown stays
// unknown, the residual every other reader in this package takes.
func LookupGateRow(data []byte, id string) (GateRow, bool) {
	idx, ok := BuildGateRowIndex(data)
	if !ok {
		return GateRow{}, false
	}
	return idx.Lookup(id)
}

// LoadGateRowFromCwd reads one row from the ledger nearest cwd.
func LoadGateRowFromCwd(cwd, targetID string) (GateRow, bool) {
	data, _, err := ReadLedgerFromCwd(cwd)
	if err != nil {
		return GateRow{}, false
	}
	return LookupGateRow(data, targetID)
}

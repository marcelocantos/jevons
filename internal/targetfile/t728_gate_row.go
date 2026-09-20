// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
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

type gateRowDoc struct {
	Targets map[string]gateRowTarget `yaml:"targets"`
}

type gateRowTarget struct {
	Status      string     `yaml:"status"`
	Achieved    string     `yaml:"achieved"`
	Attestation string     `yaml:"attestation"`
	Context     string     `yaml:"context"`
	OwnedBy     *ownedBloc `yaml:"owned_by"`
}

// LookupGateRow reads one row out of a ledger YAML document. ok is false when
// the document does not parse or does not carry that id — unknown stays
// unknown, the residual every other reader in this package takes.
func LookupGateRow(data []byte, id string) (GateRow, bool) {
	id = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "🎯"))
	if id == "" {
		return GateRow{}, false
	}
	var doc gateRowDoc
	if err := yaml.Unmarshal(data, &doc); err != nil || doc.Targets == nil {
		return GateRow{}, false
	}
	t, found := doc.Targets[id]
	if !found {
		return GateRow{}, false
	}
	row := GateRow{
		ID:          id,
		Status:      strings.TrimSpace(t.Status),
		Achieved:    strings.TrimSpace(t.Achieved),
		Attestation: t.Attestation,
		Context:     t.Context,
	}
	if t.OwnedBy != nil {
		row.OwnedByOwner = strings.TrimSpace(t.OwnedBy.Owner)
		row.OwnedByReason = t.OwnedBy.Reason
	}
	return row, true
}

// LoadGateRowFromCwd reads one row from the ledger nearest cwd.
func LoadGateRowFromCwd(cwd, targetID string) (GateRow, bool) {
	path, err := DiscoverLedgerPath(cwd)
	if err != nil {
		return GateRow{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return GateRow{}, false
	}
	return LookupGateRow(data, targetID)
}

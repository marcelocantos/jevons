// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownerquestion

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/ownergate"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// TargetLedgerAsker labels intake that came from bullseye ledger state rather
// than a worker finish-report.
const TargetLedgerAsker = "target-ledger"

// FromTargetLedger classifies a bullseye target row (owned_by / context /
// attestation) with the same tier-2/tier-3 policy as finish-report intake.
// ok is false for achieved rows, agent-owned assignments, device-only holds,
// and prose that lacks an explicit owner review/decision/input.
func FromTargetLedger(row targetfile.GateRow, workdir string) (Question, *ReviewEvent, bool, error) {
	if row.ID == "" || row.IsAchieved() || strings.TrimSpace(row.Status) == "set_aside" {
		return Question{}, nil, false, nil
	}
	if ownergate.IsAnswered(row.OwnedByReason, row.Context) {
		return Question{}, nil, false, nil
	}
	if row.OwnedByOwner != "" && !ownergate.IsOwnerHandle(row.OwnedByOwner) {
		return Question{}, nil, false, nil
	}
	if deviceOnlyOwnerHold(row.OwnedByReason, row.Context) {
		return Question{}, nil, false, nil
	}
	payload := strings.TrimSpace(row.Context + "\n" + row.Attestation)
	blocker := strings.TrimSpace(row.OwnedByReason)
	if blocker == "" {
		blocker = firstExplicitOwnerBlockerLine(payload)
	}
	if blocker == "" && !ownergate.IsOwnerHandle(row.OwnedByOwner) {
		return Question{}, nil, false, nil
	}
	sha := firstCommitSHA(payload + "\n" + blocker)
	asker := TargetLedgerAsker

	if review, ok, err := actionableReviewFields(row.ID, sha, "", payload, blocker, workdir, asker); ok {
		q := review.Question()
		return q, &review, true, err
	}
	if q, ok, err := explicitBlockerFields(row.ID, blocker, payload, sha, workdir, asker); ok {
		return q, nil, true, err
	}
	if q, ok, err := explicitOwnerGateReason(row.ID, row.OwnedByReason, workdir, asker); ok {
		return q, nil, true, err
	}
	return Question{}, nil, false, nil
}

func firstExplicitOwnerBlockerLine(payload string) string {
	for _, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if ownerAsk.MatchString(line) {
			return line
		}
	}
	return ""
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"fmt"
	"strings"
)

// Idle-residue classes (🎯T410). These are daemon-held readings of an
// open-mission idle agent. Phase=idle alone is not enough: the same
// observation was what made the sentinel prescribe repair against workers
// that were finished-awaiting-gate or blocked-on-owner.
const (
	// IdleResidueNone: not open-mission idle residue.
	IdleResidueNone = ""
	// IdleResidueStalled: genuinely stalled — mission open, no finish /
	// owner-block evidence; nudgeable. Existing repair instruction.
	IdleResidueStalled = "stalled"
	// IdleResidueFinishedAwaitingGate: worker mission complete; target
	// still open. Prescribe closing the target to the PO — no nudge.
	IdleResidueFinishedAwaitingGate = "finished_awaiting_gate"
	// IdleResidueBlockedOnOwner: progress needs an owner action no fleet
	// repair can supply. Surface the ask; prescribe no repair.
	IdleResidueBlockedOnOwner = "blocked_on_owner"
)

// IdleResidueEvidence is daemon-held input to ClassifyIdleResidue.
// Phase=idle is carried only as IdleResidue+OpenMission; the distinction
// among stalled / finished / blocked comes from the other fields.
type IdleResidueEvidence struct {
	IdleResidue bool
	OpenMission bool
	// BoundTarget is the registry TargetID (optional; used in detail).
	BoundTarget string
	// HasBoundCommits is true when git log --grep for the bound target
	// found at least one commit.
	HasBoundCommits bool
	// TargetLedgerStatus is bullseye status when known (identified /
	// active / converging / achieved / set_aside). Empty = unknown.
	TargetLedgerStatus string
	// ReportLooksFinished is true when the report store's latest terminal
	// report classifies as finished work.
	ReportLooksFinished bool
	// OwnerAskPresent is true when a recorded owner-ask / report ask /
	// owned_by assignment / blocked_owner intent is held.
	OwnerAskPresent bool
	// IntentBlockedOwner is true when fleetintent for this agent is
	// blocked_owner (also sets OwnerAskPresent at the harness).
	IntentBlockedOwner bool
}

// IdleResidueVerdict is the named state and a compact evidence line.
type IdleResidueVerdict struct {
	Class  string
	Detail string
}

// targetStillOpen reports whether ledger status is still an open leaf.
// Achieved / set_aside are closed; empty/unknown stay open (same residual
// as LoadTargetStatusFromCwd when the row is missing).
func targetStillOpen(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "achieved", "set_aside", "retired", "done":
		return false
	default:
		return true
	}
}

// ClassifyIdleResidue names an open-mission idle agent from daemon-held
// evidence (🎯T410). Priority: blocked-on-owner → finished-awaiting-gate
// → stalled. Empty / not-residue → None.
//
// A classifier that ignored the evidence fields and always returned
// stalled would pass a never-repair check's blocked/finished arms while
// failing the stalled arm of the over-broadness oracle — and would also
// fail the finished/blocked fixtures that demand no repair.
func ClassifyIdleResidue(ev IdleResidueEvidence) IdleResidueVerdict {
	if !ev.IdleResidue || !ev.OpenMission {
		return IdleResidueVerdict{Class: IdleResidueNone}
	}
	target := strings.TrimSpace(ev.BoundTarget)
	if ev.IntentBlockedOwner || ev.OwnerAskPresent {
		detail := "blocked on owner"
		if target != "" {
			detail = fmt.Sprintf("blocked on owner target=%s", target)
		}
		if ev.IntentBlockedOwner {
			detail += " intent=blocked_owner"
		}
		return IdleResidueVerdict{
			Class:  IdleResidueBlockedOnOwner,
			Detail: detail,
		}
	}
	// Finished: report store claims done, or bound-target commits exist
	// while the ledger leaf is still open. Target already achieved is not
	// "awaiting gate" — that agent is residue of a different kind.
	finished := ev.ReportLooksFinished ||
		(ev.HasBoundCommits && targetStillOpen(ev.TargetLedgerStatus))
	if finished && targetStillOpen(ev.TargetLedgerStatus) {
		detail := "finished awaiting gate"
		if target != "" {
			detail = fmt.Sprintf("finished awaiting gate target=%s", target)
		}
		parts := make([]string, 0, 3)
		if ev.ReportLooksFinished {
			parts = append(parts, "report=finished")
		}
		if ev.HasBoundCommits {
			parts = append(parts, "commits=yes")
		}
		if st := strings.TrimSpace(ev.TargetLedgerStatus); st != "" {
			parts = append(parts, "ledger="+st)
		}
		if len(parts) > 0 {
			detail += " " + strings.Join(parts, " ")
		}
		return IdleResidueVerdict{
			Class:  IdleResidueFinishedAwaitingGate,
			Detail: detail,
		}
	}
	detail := "open-mission idle residue — stalled"
	if target != "" {
		detail = fmt.Sprintf("open-mission idle residue — stalled target=%s", target)
	}
	return IdleResidueVerdict{
		Class:  IdleResidueStalled,
		Detail: detail,
	}
}

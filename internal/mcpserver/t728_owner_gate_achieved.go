// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/ownergate"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// Recording a 🎯T449 owner gate on a row that is already achieved (🎯T728).
//
// WHY THIS IS A SEPARATE SEQUENCE AND NOT A FLAG. bullseye refuses `owner` on a
// terminal status, and the refusal cannot be routed around inside one apply:
// the owner assignment is applied before the status mutation in the same
// fragment, so a single `--set status=identified --set owner=owner` still reads
// the OLD status when it checks, and is still refused. The ordering is a
// one-line fix in bullseye's `apply.rs` — and worth making, so it is filed
// there — but 0.56.0 is what is on PATH, and 🎯T720 is the precedent: that
// target shipped what the installed CLI already accepts rather than waiting on
// a release, because the outage it fixed was live. Same judgment here. Two
// applies work tonight against the CLI as installed; when the ordering lands,
// this collapses to one and the ceremony does not change shape.
//
// WHAT THE SEQUENCE PROTECTS. Between the reopen and the assignment there is a
// window where the row is active, finished, and unparked — the exact state
// 🎯T449 exists to prevent, because frontier-consume would spawn an implementer
// against it. So the assignment failing is not reported and left: the row is
// put back the way it was found, with its attestation, and the caller is told
// the gate is unrecorded. Each apply also carries `if_status`, so neither step
// can act on a row another writer moved underneath it.

// ownerGateReopenArgs reopens an achieved row so it can hold a gate. `reason`
// is required by the transition and lands in the row's context audit, which is
// what makes the reopen a permanent record rather than a silent edit.
func ownerGateReopenArgs(cwd, target, reason string) []string {
	return []string{
		"apply",
		"--cwd", cwd,
		"--id", target,
		"--set", "if_status=achieved",
		"--set", "status=identified",
		"--set", "reason=" + reason,
	}
}

// ownerGateAssignReopenedArgs is the assignment half, guarded on the status the
// reopen just established.
func ownerGateAssignReopenedArgs(cwd, target, reason string) []string {
	return []string{
		"apply",
		"--cwd", cwd,
		"--id", target,
		"--set", "if_status=identified",
		"--set", "owner=" + ownergate.OwnerHandle,
		"--set", "reason=" + reason,
	}
}

// ownerGateReAchieveArgs re-achieves a reopened row. One apply: the status
// change clears the owner assignment by 🎯T64 hygiene, so the gate is answered
// and the achievement restored without a window in between where the row is
// neither parked nor closed.
func ownerGateReAchieveArgs(cwd, target, fromStatus, attestation string) []string {
	return []string{
		"apply",
		"--cwd", cwd,
		"--id", target,
		"--set", "if_status=" + fromStatus,
		"--set", "status=achieved",
		"--set", "attestation=" + attestation,
	}
}

// recordGateOnAchievedRow performs the reopen ceremony for op=record.
func recordGateOnAchievedRow(cwd, target string, rec ownergate.Record, row targetfile.GateRow) (*mcp.CallToolResult, error) {
	reopen := ownergate.Reopen{Record: rec, AchievedOn: row.Achieved, Attestation: row.Attestation}
	reopenReason, err := reopen.Reason()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("owner gate refused (🎯T449): %v", err)), nil
	}
	gateReason, err := reopen.GateReason()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("owner gate refused (🎯T449): %v", err)), nil
	}

	// The load-bearing precondition, checked before anything is written: the
	// reopen clears `attestation`, so unless the text this ceremony is about to
	// write carries it back byte for byte, the round trip would trade the
	// oracle evidence for a queryable verdict. That is a worse ledger than the
	// prose workaround, so it is refused rather than attempted.
	att := strings.TrimSpace(row.Attestation)
	if att != "" && ownergate.PreservedAttestation(gateReason) != att {
		return mcp.NewToolResultError(fmt.Sprintf(
			"owner gate refused (🎯T728): reopening 🎯%s would not carry its attestation forward intact, and a ceremony "+
				"that loses the oracle evidence is worse than leaving the residue in prose. The row is untouched.",
			target)), nil
	}

	out, err := runBullseye(ownerGateReopenArgs(cwd, target, reopenReason)...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(
			"bullseye reopen failed — 🎯%s is untouched and the gate is NOT recorded: %v\n%s", target, err, out)), nil
	}

	assigned, aerr := runBullseye(ownerGateAssignReopenedArgs(cwd, target, gateReason)...)
	if aerr != nil {
		restore := att
		if restore == "" {
			restore = ownergate.AttestationFromAchieveAudit(row.Context)
		}
		back, berr := runBullseye(ownerGateReAchieveArgs(cwd, target, "identified", restore)...)
		if berr != nil {
			return mcp.NewToolResultError(fmt.Sprintf(
				"🎯%s IS REOPENED AND UNPARKED AND NEEDS A HAND (🎯T728). The assignment failed: %v\n%s\n"+
					"Putting the achievement back also failed: %v\n%s\n"+
					"Until someone re-achieves it or records the gate, frontier-consume sees finished work as ready. "+
					"The attestation is in the reopen audit in its context, after %q.",
				target, aerr, assigned, berr, back, ownergate.MarkerPreserved)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf(
			"owner gate not recorded — the assignment failed and 🎯%s was put back as achieved with its attestation: %v\n%s",
			target, aerr, assigned)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf(
		"🎯%s was achieved %s; it is now %s and reopened to hold the gate (🎯T728).\n\n"+
			"This is the sanctioned reopen, not a weakening of achieved immutability: an achieved row can hold neither an "+
			"owner assignment nor a content edit, and a row reading achieved while a real owner verdict is outstanding is "+
			"the misstatement this ceremony exists to correct. Frontier-consume parks it; nothing will spawn against it.\n\n"+
			"On `op=answer verdict=accept` the row is re-achieved and this attestation is restored verbatim. On reject it "+
			"stays active and work resumes from the landed commit.\n\n"+
			"Residue, by design: the `achieved` date cannot be restored — `bullseye apply` has no key for it — so a "+
			"re-achieve stamps the day the owner answered. The original date is named in both texts and in the context audit.\n\n"+
			"Reason written:\n%s\n\n%s\n%s",
		target, row.Achieved, ownergate.MarkerAwaiting, gateReason, out, assigned)), nil
}

// answerGateOnReopenedRow answers a gate that the ceremony recorded on a row it
// reopened, and so owes that row its achievement back when the owner accepts.
func answerGateOnReopenedRow(cwd, target string, verdict ownergate.Verdict, note, by string, row targetfile.GateRow) (*mcp.CallToolResult, error) {
	if verdict == ownergate.VerdictReject {
		// Reject is the honest active state: the owner said no, so the row is
		// not achieved. Unassign and leave it, exactly as for a row that was
		// never achieved in the first place.
		out, err := runBullseye(ownerGateAnswerArgs(cwd, target)...)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("bullseye unassign failed: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf(
			"🎯%s gate answered — %s\nThe row stays active and is NOT re-achieved: the owner rejected it, so achieved would "+
				"be false. Resume from the landed commit — do not restart from scratch. The original attestation is in the "+
				"reopen audit in its context, after %q.\n\n%s",
			target, ownergate.FormatAnswer(verdict, note, by, time.Now()), ownergate.MarkerPreserved, out)), nil
	}

	preserved, source := ownergate.PreservedAttestation(row.OwnedByReason), "the recorded gate"
	if preserved == "" {
		preserved, source = ownergate.PreservedAttestation(row.Context), "the reopen audit in context"
	}
	if preserved == "" {
		preserved, source = ownergate.AttestationFromAchieveAudit(row.Context),
			"bullseye's own achieve audit, which stops at a blank line and may be truncated"
	}
	attestation := ownergate.RestoreAttestation(verdict, note, by,
		ownergate.ReopenedAchievedDate(row.OwnedByReason), preserved, time.Now())

	out, err := runBullseye(ownerGateReAchieveArgs(cwd, target, row.Status, attestation)...)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf(
			"🎯%s is STILL REOPENED AND PARKED — the owner accepted but re-achieving failed: %v\n%s\n"+
				"The gate assignment is intact, so nothing will spawn against it; re-run op=answer.", target, err, out)), nil
	}
	if preserved == "" {
		return mcp.NewToolResultText(fmt.Sprintf(
			"🎯%s re-achieved (🎯T728), but its ORIGINAL ATTESTATION COULD NOT BE RECOVERED from the row — read it from "+
				"the git history of the ledger and patch it back. Verdict: %s\n\n%s",
			target, ownergate.FormatAnswer(verdict, note, by, time.Now()), out)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"🎯%s re-achieved — the owner accepted the 🎯T449 gate and the original attestation is restored verbatim from %s (🎯T728).\n"+
			"The `achieved` date is today, not the day the work landed: `bullseye apply` has no key for it, and the "+
			"original date is named in the new attestation and in the context audit.\n\n%s",
		target, source, out)), nil
}

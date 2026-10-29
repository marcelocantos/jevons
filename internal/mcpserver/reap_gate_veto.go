// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T766.4.1: the reap path's false-green veto reads typed facts only.
//
// 🎯T470 made a contradicted claim unreapable: jv-t391 was flagged
// attestation_not_green and reaped finished_work 447ms later. 🎯T1027 added
// the foreign-repo half. Both used to run through gate.FlagFalseGreen, whose
// rules read English — GATE lines in the payload, "| tail", "exit=" with no
// number, failure and removal framing, "all tests pass". That is the
// report-time banner's job (false_green.go): a reader judges the report. A
// reap is a control, and a control does not decide by a regular expression
// over what a worker wrote.
//
// What the reap may read:
//
//   - the envelope's verdict slot: a declared finish whose own verdict is
//     not GREEN does not support its claim;
//   - the record behind the declared oracle gate-id, looked up by id in the
//     gate store (~/.jevons/gates): missing, KILLED, not GREEN, or exit ≠ 0
//     vetoes — unless a gate-role slot declares that id a control;
//   - the measured commit of every declared gate id (oracle and gate-role),
//     asked of the worker's own repository: a commit it does not hold is
//     another repo's run (🎯T1027), whatever its verdict.
//
// A GATE line in the payload is prose and is not consulted. A report whose
// only evidence is prose is not a declared finish, so ClassifyDone already
// kept it.

// reapGateVeto is ShouldAutoReapDoneWorkAgent's 🎯T470 / 🎯T1027 check.
// workDir scopes the foreign-repo check; "" or a non-repo skips that half.
func reapGateVeto(report, workDir string) []gate.Flag {
	m, _ := envelope.Parse(report)
	if m == nil {
		return nil
	}
	var lookup func(string) (*gate.Record, bool)
	if store := gateStore(); store != nil {
		lookup = store.Lookup
	}
	known, _ := gate.CommitKnownIn(workDir)
	flags := append(TypedGateFlags(m, lookup, known), gate.FlagLandingSHA(m.SHA, workDir)...)
	return append(flags, gate.CheckOwnerImpactMissing(m, workDir)...)
}

// TypedGateFlags is the pure decision behind reapGateVeto: the ways an
// envelope's declared evidence fails to support a finish, from its slots
// and the records they name. lookup nil (no store) skips the record checks;
// known nil (no repo to scope to) skips the foreign-repo check. Neither
// degrades to reading the payload.
func TypedGateFlags(m *envelope.Message, lookup func(string) (*gate.Record, bool), known gate.CommitKnownFunc) []gate.Flag {
	if m == nil {
		return nil
	}
	var flags []gate.Flag
	if m.Verdict != envelope.VerdictNone && !m.Verdict.IsPass() {
		flags = append(flags, gate.Flag{
			Kind:     gate.FlagAttestationNotGreen,
			Detail:   fmt.Sprintf("the envelope's own verdict slot is %s, which is not a pass", m.Verdict),
			Evidence: "jevons: verdict " + string(m.Verdict),
		})
	}
	controls := map[string]bool{}
	for _, r := range m.GateRoles {
		if r.IsControl() {
			controls[r.ID] = true
		}
	}
	var ids []string
	evidence := map[string]string{}
	if m.GateID != "" {
		if !controls[m.GateID] {
			flags = append(flags, envelopeGateFlags(m.GateID, lookup)...)
		}
		ids = append(ids, m.GateID)
		evidence[m.GateID] = "jevons: oracle gate-id=" + m.GateID
	}
	for _, r := range m.GateRoles {
		if _, seen := evidence[r.ID]; seen || r.ID == "" {
			continue
		}
		ids = append(ids, r.ID)
		evidence[r.ID] = "jevons: gate-role " + r.ID + " " + r.Role
	}
	return append(flags, gate.FlagForeignIDs(ids, evidence, lookup, known)...)
}

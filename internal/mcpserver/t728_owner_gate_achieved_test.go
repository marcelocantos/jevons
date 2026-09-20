// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/ownergate"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// The attestation under test is several paragraphs, like every real one in this
// ledger. 🎯T711's is 2.6 KB across three. A round trip that recovered only the
// first paragraph would satisfy every non-empty assertion while destroying the
// oracle evidence, so the assertions below compare the whole string.
const t728FixtureAttestation = `Achieved 2026-09-21 by jevons-po on the hermetic clauses.

ORACLE: GATE exit=0 GREEN id=45acfeb7, tree=clean@6e9da8f5a096, covering TestT711
and the deliver/queue family.

RESIDUAL, owner-only class 3, explicitly NOT discharged: the owner has not
confirmed the live specimen behaves as intended.`

const t728Evidence = "landed at 6e9da8f5; GATE id=45acfeb7 GREEN over TestT711"

func t728RecordReq(cwd, target string) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"cwd":      cwd,
		"target":   target,
		"op":       "record",
		"question": "Does the live seat preempt an in-flight turn as intended?",
		"evidence": t728Evidence,
		"by":       "jevons-po",
	}
	return req
}

// TestT728GateOnAchievedRowRoundTripsTheAttestation is the deliverable: a gate
// recorded on an achieved row, answered, and the row re-achieved with its
// attestation intact byte for byte. A ceremony that loses the evidence on the
// way to recording a verdict would be worse than the prose it replaces, so the
// comparison is the whole string and not a non-empty check.
func TestT728GateOnAchievedRowRoundTripsTheAttestation(t *testing.T) {
	requireBullseye(t)
	s := New(t.TempDir(), nil, nil)
	repo, id := t728AchievedFixture(t)

	res, err := s.handleOwnerGate(context.Background(), t728RecordReq(repo, id))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("record on an achieved row was refused — that is the 🎯T728 defect: %s", targetFileToolText(res))
	}

	row, ok := targetfile.LoadGateRowFromCwd(repo, id)
	if !ok {
		t.Fatal("row vanished after record")
	}
	if row.IsAchieved() {
		t.Fatalf("row is still achieved, so it cannot hold the gate: status=%q owner=%q", row.Status, row.OwnedByOwner)
	}
	if row.OwnedByOwner != ownergate.OwnerHandle {
		t.Fatalf("owned_by.owner=%q, want %s — the residue is not recorded as state", row.OwnedByOwner, ownergate.OwnerHandle)
	}
	for _, marker := range []string{ownergate.MarkerAwaiting, ownergate.MarkerReopened} {
		if !strings.Contains(row.OwnedByReason, marker) {
			t.Fatalf("gate reason is missing %q: %q", marker, row.OwnedByReason)
		}
	}
	if got := ownergate.PreservedAttestation(row.OwnedByReason); got != t728FixtureAttestation {
		t.Fatalf("the reopen did not carry the attestation into the gate reason.\n got %q\nwant %q",
			got, t728FixtureAttestation)
	}
	if got := ownergate.PreservedAttestation(row.Context); got != t728FixtureAttestation {
		t.Fatalf("the reopen audit did not carry the attestation into context.\n got %q\nwant %q",
			got, t728FixtureAttestation)
	}

	// The whole point of recording it as state: nothing spawns against it.
	leaves, _, err := targetfile.LoadFrontierLeavesFromCwd(repo)
	if err != nil {
		t.Fatal(err)
	}
	leaf, found := t720Leaf(leaves, id)
	if !found {
		t.Fatal("the reopened row is not a frontier leaf at all, so no reader parks it")
	}
	kind := poproactive.ClassifyLeaf(poproactive.LeafObs{
		ID:            leaf.ID,
		Name:          leaf.Name,
		OwnedBy:       leaf.OwnedBy,
		OwnedByReason: leaf.OwnedByReason,
	})
	if kind != poproactive.LeafSkipAwaitingOwnerVerdict {
		t.Fatalf("reopened row classified %s — an implementer would be spawned against finished work", kind)
	}

	res, err = s.handleOwnerGate(context.Background(), t720AnswerReq(repo, id, "accept"))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("accept: %s", targetFileToolText(res))
	}

	row, ok = targetfile.LoadGateRowFromCwd(repo, id)
	if !ok {
		t.Fatal("row vanished after accept")
	}
	if !row.IsAchieved() {
		t.Fatalf("accept left the row active (status=%q) — finished work reading as ready is the 🎯T449 defect one step later", row.Status)
	}
	if row.OwnedByOwner != "" {
		t.Fatalf("accept left the row parked with %q", row.OwnedByOwner)
	}
	if got := ownergate.PreservedAttestation(row.Attestation); got != t728FixtureAttestation {
		t.Fatalf("THE ATTESTATION DID NOT SURVIVE THE ROUND TRIP.\n got %q\nwant %q", got, t728FixtureAttestation)
	}
	if !strings.Contains(row.Attestation, t728FixtureAttestation) {
		t.Fatal("the original attestation is not present verbatim in the re-achieved row")
	}
}

// Reject is the honest active state: the owner said no, so achieved would be a
// lie. The row stays open and unassigned, with the attestation still readable.
func TestT728RejectLeavesTheRowActiveAndUnassigned(t *testing.T) {
	requireBullseye(t)
	s := New(t.TempDir(), nil, nil)
	repo, id := t728AchievedFixture(t)

	if res, err := s.handleOwnerGate(context.Background(), t728RecordReq(repo, id)); err != nil {
		t.Fatal(err)
	} else if res.IsError {
		t.Fatalf("record: %s", targetFileToolText(res))
	}
	if res, err := s.handleOwnerGate(context.Background(), t720AnswerReq(repo, id, "reject")); err != nil {
		t.Fatal(err)
	} else if res.IsError {
		t.Fatalf("reject: %s", targetFileToolText(res))
	}

	row, ok := targetfile.LoadGateRowFromCwd(repo, id)
	if !ok {
		t.Fatal("row vanished after reject")
	}
	if row.IsAchieved() {
		t.Fatal("reject re-achieved the row")
	}
	if row.OwnedByOwner != "" {
		t.Fatalf("reject left the row parked with %q", row.OwnedByOwner)
	}
	if got := ownergate.PreservedAttestation(row.Context); got != t728FixtureAttestation {
		t.Fatalf("reject lost the attestation from the audit.\n got %q\nwant %q", got, t728FixtureAttestation)
	}
}

// An ordinary active row must keep taking the one-apply path: the reopen
// ceremony is for achieved rows only, and must not creep.
func TestT728ActiveRowStillTakesTheSingleApplyPath(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })
	var calls [][]string
	runBullseye = func(args ...string) (string, error) {
		calls = append(calls, append([]string{}, args...))
		return "ok: true\n", nil
	}
	s := New(t.TempDir(), nil, nil)
	// No ledger in this cwd, so the row read comes back unknown — unknown must
	// behave like active, not like achieved.
	res, err := s.handleOwnerGate(context.Background(), t728RecordReq(t.TempDir(), "T2"))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("record: %s", targetFileToolText(res))
	}
	if len(calls) != 1 {
		t.Fatalf("want one apply for an active row, got %d: %v", len(calls), calls)
	}
	if strings.Contains(strings.Join(calls[0], " "), "status=identified") {
		t.Fatalf("an active row was reopened: %v", calls[0])
	}
}

// The 🎯T720 discipline, extended to the flags this ceremony adds: the installed
// CLI is probed, so a rejected flag is RED at test time and not when a PO is
// closing a target.
func TestT728ReopenFlagsMatchInstalledCLI(t *testing.T) {
	requireBullseye(t)
	cases := [][]string{
		ownerGateReopenArgs(t.TempDir(), "T2", "reason"),
		ownerGateAssignReopenedArgs(t.TempDir(), "T2", "reason"),
		ownerGateReAchieveArgs(t.TempDir(), "T2", "identified", "attestation"),
	}
	for _, args := range cases {
		accepted := probeAcceptedFlags(t, args[0])
		for _, name := range flagNames(args) {
			if _, ok := accepted[name]; !ok {
				t.Errorf("tool passes %s to bullseye %s, which rejects it: accepted %v",
					name, args[0], acceptedKeys(accepted))
			}
		}
	}
}

// t728AchievedFixture tracks a target in a throwaway ledger and achieves it
// with a multi-paragraph attestation — the state 🎯T711 was in when the gate
// became unrecordable.
func t728AchievedFixture(t *testing.T) (repo, id string) {
	t.Helper()
	repo, id = t720FixtureTarget(t)
	achieve := exec.Command("bullseye", "apply",
		"--cwd", repo,
		"--id", id,
		"--set", "status=achieved",
		"--set", "attestation="+t728FixtureAttestation)
	if out, err := achieve.CombinedOutput(); err != nil {
		t.Fatalf("bullseye achieve: %v\n%s", err, out)
	}
	row, ok := targetfile.LoadGateRowFromCwd(repo, id)
	if !ok || !row.IsAchieved() {
		t.Fatalf("fixture is not achieved: %+v", row)
	}
	// The ledger's own round trip is the floor for everything below: if YAML
	// does not return the attestation verbatim, nothing this ceremony does can.
	if row.Attestation != t728FixtureAttestation {
		t.Fatalf("ledger round trip already altered the attestation.\n got %q\nwant %q",
			row.Attestation, t728FixtureAttestation)
	}
	if row.Achieved == "" {
		t.Fatal("fixture has no achieved date")
	}
	return repo, id
}

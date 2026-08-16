// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/ownergate"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T449 clause 3, end to end: ledger on disk → frontier load → classify →
// sweep. This is the path that spawned `jv-t383-auto` against finished work
// twice in one session, so the oracle drives the whole of it rather than the
// classifier alone.
//
// The fixture is 🎯T383's real shape. T601 is the control that keeps the fix
// honest — identical status, identical name, no assignment — and T602 is the
// answered gate, which must come back to normal handling.
const t449OwnerGateLedger = `
targets:
  T600:
    name: Sticky fleet-tree selection
    status: identified
    acceptance:
    - "The selected aside stays selected indefinitely"
    owned_by:
      owner: owner
      reason: >-
        BUILT, AWAITING OWNER VERDICT — recorded 2026-08-15 by jevons-po
        (🎯T449). Owner gate: after a hard reload, does the second aside stay
        selected? Evidence: landed at 8f25010; GATE node id=adf36ad8 GREEN.
        Do not spawn an implementer.
  T601:
    name: Sticky fleet-tree selection
    status: identified
    acceptance:
    - "The selected aside stays selected indefinitely"
  T602:
    name: Gate the owner has answered
    status: identified
    owned_by:
      owner: owner
      reason: >-
        BUILT, AWAITING OWNER VERDICT — recorded 2026-08-15 by jevons-po
        (🎯T449). Evidence: landed at 8f25010. OWNER VERDICT ANSWERED
        (reject) 2026-08-15 recorded by jevons-po: still jumps on the second
        aside.
`

func t449Sweep(t *testing.T) map[string]FrontierConsumeReport {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t449OwnerGateLedger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}

	s := New(dir, nil, nil)
	s.SetRegistry(reg)

	ledger, err := OpenFrontierSpawnLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:   s,
		Workdir:  dir,
		ParentPO: "jevons-po",
		// Both controls must be able to spawn in the one sweep, or the
		// per-cycle cap would park the second one and the oracle would read
		// a capacity park as proof of the fix.
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			return reg.Register(claudia.AgentDef{
				Name: workerName, WorkDir: dir, SessionID: "spawned",
				Purpose: claudia.PurposeWork, Parent: parent, TargetID: leaf.ID,
			})
		},
	}, ledger)

	byTarget := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byTarget[r.TargetID] = r
	}
	// The whole point of the target: no implementer exists for the gated leaf.
	if def := reg.Def("jv-t600-auto"); def != nil {
		t.Fatalf("an implementer was spawned against finished work: %+v", def)
	}
	return byTarget
}

func TestT449SweepParksAwaitingOwnerVerdictLeaf(t *testing.T) {
	byTarget := t449Sweep(t)

	r := byTarget["T600"]
	if r.Action != FrontierConsumePark || r.Reason != FrontierReasonAwaitingOwnerVerdict {
		t.Fatalf("T600: action=%s reason=%s, want park/%s (%+v)",
			r.Action, r.Reason, FrontierReasonAwaitingOwnerVerdict, r)
	}
	// Parking silently would leave the owner reading a bare code where the
	// answer they owe is. The park detail carries the recorded claim.
	if !strings.Contains(r.Err, ownergate.MarkerAwaiting) || !strings.Contains(r.Err, "8f25010") {
		t.Fatalf("T600 park detail drops the recorded claim: %q", r.Err)
	}
}

// Control 1: identical status, identical name, no assignment — still spawns.
// Without this the fix could be "park everything" and pass.
func TestT449SweepControlUntouchedLeafStillSpawns(t *testing.T) {
	byTarget := t449Sweep(t)
	if r := byTarget["T601"]; r.Action != FrontierConsumeSpawn || r.Worker != "jv-t601-auto" {
		t.Fatalf("untouched control: %+v, want spawn jv-t601-auto", r)
	}
}

// Control 2: the owner answered, so the leaf is ordinary work again.
func TestT449SweepControlAnsweredGateSpawnsAgain(t *testing.T) {
	byTarget := t449Sweep(t)
	if r := byTarget["T602"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("answered gate: action=%s reason=%s, want spawn (%+v)", r.Action, r.Reason, r)
	}
}

// The park reason is distinguishable from every other park reason in the
// sweep's vocabulary. Every other one means "not started"; this one means
// "finished, your turn", and a reader collapsing them is the defect.
func TestT449ParkReasonIsItsOwnVocabulary(t *testing.T) {
	seen := map[string]string{}
	for name, reason := range map[string]string{
		"design_gated":    FrontierReasonDesignGated,
		"engaged":         FrontierReasonEngaged,
		"blocked":         FrontierReasonBlocked,
		"closed":          FrontierReasonClosed,
		"set_aside_dep":   FrontierReasonSetAsideDep,
		"deferred":        FrontierReasonDeferred,
		"owner_parked":    FrontierReasonOwnerParked,
		"capacity":        FrontierReasonCapacity,
		"awaiting_owner":  FrontierReasonAwaitingOwnerVerdict,
		"owned_by_other":  FrontierReasonOwnedByOther,
		"spawn_halted":    FrontierReasonSpawnHalted,
		"po_unregistered": FrontierReasonPOMissing,
	} {
		if prev, dup := seen[reason]; dup {
			t.Fatalf("%s and %s share the reason code %q", name, prev, reason)
		}
		seen[reason] = name
	}
	if poproactive.LeafSkipAwaitingOwnerVerdict.String() == poproactive.LeafSkipOwnedByOther.String() {
		t.Fatal("the two 🎯T449 kinds render identically")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/staffops"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T431: T370-shaped assignment (owned_by set, no 🎯T449 awaiting-verdict
// marker) plus a companion unassigned leaf. The skip cannot be achieved by
// disabling auto-spawn: the companion must still spawn, and the assigned
// leaf must not count as unconsumed frontier work in the sentinel depth.

const t431OwnerAssignedLedger = `
schema_version: 1
project: test
targets:
  T370:
    name: keypress residual
    status: identified
    acceptance:
    - "needs a real owner keypress"
    owned_by:
      owner: owner
      reason: Assigning so frontier-consume stops re-spawning workers onto it
  T500:
    name: ordinary ready Build leaf
    status: identified
    acceptance:
    - "ship hermetic fix"
`

func t431WriteLedger(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t431OwnerAssignedLedger), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestT431SweepParksOwnerAssignedCompanionStillSpawns(t *testing.T) {
	dir := t431WriteLedger(t)
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
	var spawned []string
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:            s,
		Workdir:           dir,
		ParentPO:          "jevons-po",
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			spawned = append(spawned, leaf.ID)
			return reg.Register(claudia.AgentDef{
				Name: workerName, WorkDir: dir, SessionID: "spawned",
				Purpose: claudia.PurposeWork, Parent: parent, TargetID: leaf.ID,
			})
		},
	}, nil)
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}

	r370 := byID["T370"]
	if r370.Action == FrontierConsumeSpawn {
		t.Fatalf("T370 owned_by must not spawn: %+v", r370)
	}
	if r370.Action != FrontierConsumePark {
		t.Fatalf("T370: action=%s reason=%s, want park (%+v)", r370.Action, r370.Reason, r370)
	}
	if r370.Reason != FrontierReasonAwaitingOwnerVerdict && r370.Reason != FrontierReasonOwnedByOther {
		t.Fatalf("T370 park reason=%s, want owner-assignment park", r370.Reason)
	}
	if def := reg.Def("jv-t370-auto"); def != nil {
		t.Fatalf("an implementer was spawned against an owner-assigned leaf: %+v", def)
	}

	r500 := byID["T500"]
	if r500.Action != FrontierConsumeSpawn || r500.Worker != "jv-t500-auto" {
		t.Fatalf("unassigned companion must spawn: %+v", r500)
	}
	for _, id := range spawned {
		if id == "T370" {
			t.Fatal("T370 must not be spawned")
		}
	}
	if len(spawned) != 1 || spawned[0] != "T500" {
		t.Fatalf("spawned=%v want [T500] — skip must not disable auto-spawn", spawned)
	}
}

func TestT431SentinelDepthExcludesOwnerAssignedLeaf(t *testing.T) {
	dir := t431WriteLedger(t)
	s := New("/tmp", nil, nil)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	sigs, resources := s.sampleSentinel(SentinelLoopArgs{Server: s, Workdir: dir}, now)
	if resources.FrontierDepth != 1 {
		t.Fatalf("frontier_depth=%d want 1 (T500 only; T370 is owner-assigned)", resources.FrontierDepth)
	}
	var stall *staffops.Signal
	for i := range sigs {
		if sigs[i].Kind == "frontier_stall" {
			stall = &sigs[i]
			break
		}
	}
	if stall == nil {
		t.Fatalf("expected frontier_stall on the unassigned companion; sigs=%+v", sigs)
	}
	if !strings.Contains(stall.Detail, "T500") {
		t.Fatalf("stall must cite the unassigned leaf T500: %q", stall.Detail)
	}
	if strings.Contains(stall.Detail, "T370") {
		t.Fatalf("stall must not count owner-assigned T370 as unconsumed: %q", stall.Detail)
	}
}

func TestT431SweepUnitOwnedByParksCompanionSpawns(t *testing.T) {
	spawned := 0
	var spawnedIDs []string
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{
				ID:            "T370",
				Name:          "keypress residual",
				OwnedBy:       "owner",
				OwnedByReason: "Assigning so frontier-consume stops re-spawning workers onto it",
			},
			{ID: "T500", Name: "ordinary ready Build leaf"},
		},
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf poproactive.LeafObs, _ string) error {
			spawned++
			spawnedIDs = append(spawnedIDs, leaf.ID)
			return nil
		},
	})
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	if byID["T370"].Action != FrontierConsumePark {
		t.Fatalf("T370: %+v want park", byID["T370"])
	}
	if byID["T500"].Action != FrontierConsumeSpawn {
		t.Fatalf("T500 companion: %+v want spawn", byID["T500"])
	}
	if spawned != 1 || spawnedIDs[0] != "T500" {
		t.Fatalf("spawned=%v want [T500]", spawnedIDs)
	}
}

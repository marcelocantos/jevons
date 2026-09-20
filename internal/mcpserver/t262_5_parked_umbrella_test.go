// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T262.5: Sweep parks/skips T254.2 under a parked T254; does not spawn it.
func TestSweepFrontierConsumeParkedAncestorSkipNotSpawn(t *testing.T) {
	spawned := 0
	var spawnedIDs []string
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{
				ID: "T254.2", Name: "worktrees",
				ParkedAncestors: []string{"T254"},
			},
			{
				ID: "T254.3", Name: "plan steps",
				ParkedAncestors: []string{"T254"},
			},
			{ID: "T262.5", Name: "umbrella skip leaf"},
			{ID: "T500", Name: "ordinary ready"},
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
	r := byID["T254.2"]
	if r.Action != FrontierConsumeSkip || r.Reason != FrontierReasonParkedAncestor {
		t.Fatalf("T254.2: action=%s reason=%s want skip/skip_parked_ancestor (%+v)", r.Action, r.Reason, r)
	}
	if r.Err == "" || r.Err != "parked ancestor: T254" {
		t.Fatalf("T254.2 err=%q want parked ancestor: T254", r.Err)
	}
	if byID["T254.3"].Action != FrontierConsumeSkip || byID["T254.3"].Reason != FrontierReasonParkedAncestor {
		t.Fatalf("T254.3: %+v", byID["T254.3"])
	}
	if byID["T262.5"].Action != FrontierConsumeSpawn {
		t.Fatalf("T262.5 (design-discussion parent) must still spawn: %+v", byID["T262.5"])
	}
	if byID["T500"].Action != FrontierConsumeSpawn {
		t.Fatalf("ordinary ready must spawn: %+v", byID["T500"])
	}
	for _, id := range spawnedIDs {
		if id == "T254.2" || id == "T254.3" {
			t.Fatalf("factory child %s must not spawn", id)
		}
	}
	if spawned != 2 {
		t.Fatalf("spawned=%d want 2 (T262.5 + T500)", spawned)
	}
}

// 🎯T262.5 assembly: ledger parked T254 + unblocked T254.2 never auto-spawns
// the child; achieved/set_aside parked parents do not skip; T262.5 stays ready.
func TestFrontierConsumeSweepAssemblyParkedAncestor(t *testing.T) {
	const ledger = `
targets:
  T254:
    name: factory parked
    status: converging
    tags:
    - parked
    depends_on:
    - T254.2
    - T254.3
  T254.2:
    name: worktrees
    status: identified
  T254.3:
    name: plan steps
    status: identified
  T262:
    name: frontier design
    status: converging
    tags:
    - design-discussion
    depends_on:
    - T262.5
  T262.5:
    name: umbrella skip
    status: identified
  T100:
    name: achieved parked parent
    status: achieved
    tags:
    - parked
  T100.1:
    name: child of achieved parent
    status: identified
  T200:
    name: set_aside parked parent
    status: set_aside
    tags:
    - parked-for-design
  T200.1:
    name: child of set_aside parent
    status: identified
  T300:
    name: own parked tag
    status: identified
    tags:
    - parked
  T500:
    name: ordinary ready
    status: identified
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
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
	var spawned []string
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:            s,
		Workdir:           dir,
		ParentPO:          "jevons-po",
		MaxSpawnsPerCycle: 8,
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
	if r := byID["T254.2"]; r.Action != FrontierConsumeSkip || r.Reason != FrontierReasonParkedAncestor {
		t.Fatalf("T254.2 assembly: %+v", r)
	}
	if r := byID["T254.3"]; r.Action != FrontierConsumeSkip || r.Reason != FrontierReasonParkedAncestor {
		t.Fatalf("T254.3 assembly: %+v", r)
	}
	if r := byID["T300"]; r.Action != FrontierConsumeSkip || r.Reason != FrontierReasonDesignGated {
		t.Fatalf("own parked tag still skip_design: %+v", r)
	}
	if r := byID["T100.1"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("achieved parent must not skip child: %+v", r)
	}
	if r := byID["T200.1"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("set_aside parent must not skip child: %+v", r)
	}
	if r := byID["T262.5"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("T262.5 must still spawn: %+v", r)
	}
	if r := byID["T500"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("T500 must spawn: %+v", r)
	}
	for _, id := range spawned {
		if id == "T254.2" || id == "T254.3" || id == "T300" {
			t.Fatalf("must not spawn %s", id)
		}
	}
}

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

// 🎯T1105: a worktree-isolated worker bound to T<n> is engaged on T<n> in its
// clone's ledger. Specimen: 2026-10-21 09:18, the consume sweep spawned
// jv-t1104-auto beside a live jv-t1104-overlap-throttle-scout whose workdir
// was .jevons-worktrees-jevons/jv-t1104-overlap-throttle-scout.

func t1105WriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestT1105WorktreeWorkerIsEngagedOnItsTarget(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(root, "proj")
	const worker = "jv-t501-scout"
	wt := filepath.Join(root, ".jevons-worktrees-proj", worker)
	// The layout `git worktree add` writes, without running git: the linked
	// tree's .git file names its admin dir, whose commondir names the clone's
	// .git. Both trees hold the tracked ledger.
	admin := filepath.Join(clone, ".git", "worktrees", worker)
	t1105WriteFile(t, filepath.Join(clone, ".git", "HEAD"), "ref: refs/heads/master\n")
	t1105WriteFile(t, filepath.Join(admin, "commondir"), "../..\n")
	t1105WriteFile(t, filepath.Join(wt, ".git"), "gitdir: "+admin+"\n")
	t1105WriteFile(t, filepath.Join(clone, "bullseye.yaml"), frontierConsumeTestLedger)
	t1105WriteFile(t, filepath.Join(wt, "bullseye.yaml"), frontierConsumeTestLedger)

	reg, err := claudia.NewRegistry(filepath.Join(root, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: clone, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: worker, WorkDir: wt, SessionID: "s1",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T501",
	}); err != nil {
		t.Fatal(err)
	}

	s := New(clone, nil, nil)
	s.SetRegistry(reg)

	// Every engagement surface answers from the clone's scope.
	if got := workAgentsEngagedOnTarget(reg, "T501", clone, ""); len(got) != 1 || got[0] != worker {
		t.Fatalf("workAgentsEngagedOnTarget = %v, want [%s]", got, worker)
	}
	bound := workAgentsBoundOnTarget(reg, "T501", clone, "")
	if len(bound) != 1 || bound[0] != worker {
		t.Fatalf("workAgentsBoundOnTarget = %v, want [%s]", bound, worker)
	}
	seatFound := false
	for _, seat := range keepgoingSeats(reg, clone) {
		if seat.Name == worker && seat.TargetID == "T501" {
			seatFound = true
		}
	}
	if !seatFound {
		t.Fatalf("keepgoingSeats(clone) omits %s: %+v", worker, keepgoingSeats(reg, clone))
	}
	// PO proactive and the sentinel classify from frontierLeafObs over the
	// bound check; the leaf must classify engaged, not ready.
	obs := frontierLeafObs(targetfile.FrontierLeaf{ID: "T501", Name: "Already engaged leaf"}, len(bound) > 0)
	if k := poproactive.ClassifyLeaf(obs); k != poproactive.LeafSkipEngaged {
		t.Fatalf("PO proactive leaf kind = %v, want skip_engaged", k)
	}
	for _, id := range poproactive.ClassifyPOProactive([]poproactive.LeafObs{obs}).ReadyIDs {
		if id == "T501" {
			t.Fatal("PO proactive counts T501 as ready beside its worktree worker")
		}
	}

	ledger, err := OpenFrontierSpawnLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var spawned []string
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:   s,
		Workdir:  clone,
		ParentPO: "jevons-po",
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			spawned = append(spawned, leaf.ID)
			return reg.Register(claudia.AgentDef{
				Name: workerName, WorkDir: clone, SessionID: "spawned",
				Purpose: claudia.PurposeWork, Parent: parent, TargetID: leaf.ID,
			})
		},
	}, ledger)
	found := false
	for _, r := range reps {
		if r.TargetID != "T501" {
			continue
		}
		found = true
		if r.Action != FrontierConsumeSkip || r.Reason != FrontierReasonEngaged {
			t.Fatalf("sweep T501 = %+v, want skip/%s", r, FrontierReasonEngaged)
		}
	}
	if !found {
		t.Fatal("sweep did not report T501")
	}
	for _, id := range spawned {
		if id == "T501" {
			t.Fatal("sweep spawned a duplicate for T501 beside its worktree worker")
		}
	}
}

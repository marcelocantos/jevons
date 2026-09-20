// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package poproactive

import "testing"

// 🎯T262.5: a child under a parked umbrella is not auto-spawned as ready.
// The child itself may have no parked tag and no open depends_on.

func TestClassifyLeafParkedAncestorSkips(t *testing.T) {
	child := LeafObs{
		ID: "T254.2", Name: "Same-repo multi-worker fan-out uses worktrees",
		ParkedAncestors: []string{"T254"},
	}
	if k := ClassifyFrontierLeaf(child); k != LeafSkipParkedAncestor {
		t.Fatalf("parked parent + unblocked child: got %s want skip_parked_ancestor", k)
	}
	if k := ClassifyLeaf(child); k.String() != "skip_parked_ancestor" {
		t.Fatalf("string: %s", k)
	}
	// force-engage / unattended-safe do not punch through a parked umbrella.
	child.ForceEngage = true
	if k := ClassifyLeaf(child); k != LeafSkipParkedAncestor {
		t.Fatalf("force_engage must not override parked ancestor: got %s", k)
	}
	child.ForceEngage = false
	child.Tags = []string{"unattended-safe"}
	if k := ClassifyLeaf(child); k != LeafSkipParkedAncestor {
		t.Fatalf("unattended-safe must not override parked ancestor: got %s", k)
	}
}

func TestClassifyLeafAchievedParentDoesNotSkip(t *testing.T) {
	// Assembler omits achieved/set_aside ancestors from ParkedAncestors.
	// Names must not contain the design-gate substring "parked".
	child := LeafObs{ID: "T100.1", Name: "child of achieved parent"}
	if k := ClassifyLeaf(child); k != LeafReady {
		t.Fatalf("achieved parent must not skip: got %s want ready", k)
	}
	child = LeafObs{ID: "T200.1", Name: "child of set_aside parent"}
	if k := ClassifyLeaf(child); k != LeafReady {
		t.Fatalf("set_aside parent must not skip: got %s want ready", k)
	}
}

func TestClassifyLeafOwnParkedTagStillSkips(t *testing.T) {
	if k := ClassifyLeaf(LeafObs{ID: "T300", Name: "own parked", Tags: []string{"parked"}}); k != LeafSkipDesign {
		t.Fatalf("own parked tag: got %s want skip_design", k)
	}
	if k := ClassifyLeaf(LeafObs{ID: "T301", Tags: []string{"parked-for-design"}}); k != LeafSkipDesign {
		t.Fatalf("own parked-for-design: got %s want skip_design", k)
	}
}

func TestClassifyLeafDesignDiscussionParentDoesNotSkip(t *testing.T) {
	// T262 is design-discussion, not parked. T262.5 stays ready.
	child := LeafObs{ID: "T262.5", Name: "umbrella skip"}
	if k := ClassifyLeaf(child); k != LeafReady {
		t.Fatalf("design-discussion parent must not skip child: got %s want ready", k)
	}
}

func TestClassifyParkedAncestorOnlySleeps(t *testing.T) {
	d := Classify([]LeafObs{{
		ID: "T254.2", Name: "worktrees", ParkedAncestors: []string{"T254"},
	}})
	if d.Mode != Sleep || len(d.ReadyIDs) != 0 {
		t.Fatalf("factory-child-only frontier must sleep: %+v", d)
	}
	d = Classify([]LeafObs{
		{ID: "T254.2", Name: "worktrees", ParkedAncestors: []string{"T254"}},
		{ID: "T254.3", Name: "plan steps", ParkedAncestors: []string{"T254"}},
		{ID: "T262.5", Name: "umbrella skip"},
		{ID: "T500", Name: "ordinary ready"},
	})
	if d.Mode != Kick || len(d.ReadyIDs) != 2 || d.ReadyIDs[0] != "T262.5" || d.ReadyIDs[1] != "T500" {
		t.Fatalf("ready outside the parked family must kick: %+v", d)
	}
	if ShouldKeepKicking([]LeafObs{{ID: "T254.2", ParkedAncestors: []string{"T254"}}}) {
		t.Fatal("restart-woken PO must not keep kicking T254.2")
	}
}

func TestHasParkedAncestor(t *testing.T) {
	if HasParkedAncestor(LeafObs{ID: "T1"}) {
		t.Fatal("empty must be false")
	}
	if !HasParkedAncestor(LeafObs{ID: "T254.2", ParkedAncestors: []string{"T254"}}) {
		t.Fatal("T254 ancestor must count")
	}
	if HasParkedAncestor(LeafObs{ID: "T1", ParkedAncestors: []string{"  "}}) {
		t.Fatal("whitespace-only ancestor must not count")
	}
}

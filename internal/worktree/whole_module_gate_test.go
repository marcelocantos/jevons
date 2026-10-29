// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T1237: integrate and worker gates each cover a package subset; the
// standing whole-module gate is the net that catches what no subset gate
// ran. These oracles pin the three load-bearing pieces: settle (debounce by
// tip across ticks, never the in-flight commit), dedup (once per tip, not
// once per tick), and the RED/GREEN TrunkFault evidence this reuses from
// 🎯T1122 so delivery (owner notice + PO wake) needs no second path.

func TestT1237SettleDebouncesBeforeGatingATip(t *testing.T) {
	base := goModuleSharedClone(t)
	tipA := git(t, base, "rev-parse", "HEAD")

	settled, err := worktree.SettleWholeModuleGateTip(base, tipA)
	if err != nil {
		t.Fatal(err)
	}
	if settled {
		t.Fatal("first sighting of a tip must not settle immediately — the tip could still be mid-landing")
	}
	settled, err = worktree.SettleWholeModuleGateTip(base, tipA)
	if err != nil {
		t.Fatal(err)
	}
	if !settled {
		t.Fatal("seeing the same tip twice in a row must settle")
	}
}

func TestT1237SettleResetsCandidateWhenTipMoves(t *testing.T) {
	base := goModuleSharedClone(t)
	tipA := git(t, base, "rev-parse", "HEAD")
	if _, err := worktree.SettleWholeModuleGateTip(base, tipA); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(base, "other.txt"), "moved\n")
	git(t, base, "add", "other.txt")
	git(t, base, "commit", "-q", "-m", "trunk moves before settling")
	tipB := git(t, base, "rev-parse", "HEAD")

	settled, err := worktree.SettleWholeModuleGateTip(base, tipB)
	if err != nil {
		t.Fatal(err)
	}
	if settled {
		t.Fatal("a tip that moved mid-settle must restart debounce on the new tip, not settle on the stale one")
	}
}

func TestT1237SettleNeverRegatesAnAlreadyGatedTip(t *testing.T) {
	base := goModuleSharedClone(t)
	tip := git(t, base, "rev-parse", "HEAD")
	store := t.TempDir()
	if _, err := worktree.RunWholeModuleGate(base, store, tip); err != nil {
		t.Fatal(err)
	}
	// Same tip, seen again on a later tick (landings settled long ago):
	// dedup, not a second heavy run.
	settled, err := worktree.SettleWholeModuleGateTip(base, tip)
	if err != nil {
		t.Fatal(err)
	}
	if settled {
		t.Fatal("a tip already gated must not settle again")
	}
	settled, err = worktree.SettleWholeModuleGateTip(base, tip)
	if err != nil {
		t.Fatal(err)
	}
	if settled {
		t.Fatal("a tip already gated must not settle again on a repeat tick either")
	}
}

func TestT1237RedWholeModuleGatePublishesTrunkFault(t *testing.T) {
	base := goModuleSharedClone(t)
	writeFile(t, filepath.Join(base, "p1/p1_test.go"), "package p1\n\nimport \"testing\"\n\nfunc TestT1237WholeModuleRed(t *testing.T) { t.Fatal(\"whole-module regression\") }\n")
	git(t, base, "add", "p1/p1_test.go")
	git(t, base, "commit", "-q", "-m", "introduce whole-module failure")
	tip := git(t, base, "rev-parse", "HEAD")
	store := t.TempDir()

	rec, err := worktree.RunWholeModuleGate(base, store, tip)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || rec.Verdict != gate.VerdictRed {
		t.Fatalf("want RED record, got %+v", rec)
	}
	fault, err := worktree.ReadTrunkFault(base)
	if err != nil || fault == nil {
		t.Fatalf("want a published trunk fault: %+v %v", fault, err)
	}
	if fault.Kind != worktree.TrunkRed || fault.Base != tip {
		t.Fatalf("bad fault: %+v", fault)
	}
	found := false
	for _, name := range fault.Tests {
		if name == "example.com/integratetest/p1/TestT1237WholeModuleRed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fault does not name the failing test: %+v", fault.Tests)
	}
	// Re-running on the same tip must not duplicate the record under a new
	// gate id; the fault stays, carrying the same identity.
	rec2, err := worktree.RunWholeModuleGate(base, store, tip)
	if err != nil {
		t.Fatal(err)
	}
	if rec2 == nil || rec2.Verdict != gate.VerdictRed {
		t.Fatalf("want RED on repeat run too, got %+v", rec2)
	}
	fault2, err := worktree.ReadTrunkFault(base)
	if err != nil || fault2 == nil || fault2.ID != fault.ID {
		t.Fatalf("repeat run must coalesce onto the same fault identity: %+v %+v", fault, fault2)
	}
}

func TestT1237GreenWholeModuleGateClearsTrunkFault(t *testing.T) {
	base := goModuleSharedClone(t)
	writeFile(t, filepath.Join(base, "p1/p1_test.go"), "package p1\n\nimport \"testing\"\n\nfunc TestT1237WholeModuleRed(t *testing.T) { t.Fatal(\"whole-module regression\") }\n")
	git(t, base, "add", "p1/p1_test.go")
	git(t, base, "commit", "-q", "-m", "introduce whole-module failure")
	redTip := git(t, base, "rev-parse", "HEAD")
	store := t.TempDir()
	if _, err := worktree.RunWholeModuleGate(base, store, redTip); err != nil {
		t.Fatal(err)
	}
	if fault, err := worktree.ReadTrunkFault(base); err != nil || fault == nil {
		t.Fatalf("want a fault before repair: %+v %v", fault, err)
	}

	writeFile(t, filepath.Join(base, "p1/p1_test.go"), "package p1\n\nimport \"testing\"\n\nfunc TestT1237WholeModuleRed(t *testing.T) {}\n")
	git(t, base, "add", "p1/p1_test.go")
	git(t, base, "commit", "-q", "-m", "repair whole-module failure")
	greenTip := git(t, base, "rev-parse", "HEAD")

	rec, err := worktree.RunWholeModuleGate(base, store, greenTip)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || rec.Verdict != gate.VerdictGreen {
		t.Fatalf("want GREEN record, got %+v", rec)
	}
	fault, err := worktree.ReadTrunkFault(base)
	if err != nil {
		t.Fatal(err)
	}
	if fault != nil {
		t.Fatalf("GREEN whole-module gate must clear the fault, got %+v", fault)
	}
}

func TestT1237ConcurrentRunsDoNotOverlap(t *testing.T) {
	base := goModuleSharedClone(t)
	tip := git(t, base, "rev-parse", "HEAD")
	store := t.TempDir()

	var wg sync.WaitGroup
	results := make([]*gate.Record, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = worktree.RunWholeModuleGate(base, store, tip)
		}(i)
	}
	wg.Wait()
	ran := 0
	for i := range 2 {
		if errs[i] != nil {
			t.Fatalf("run %d: %v", i, errs[i])
		}
		if results[i] != nil {
			ran++
		}
	}
	if ran != 1 {
		t.Fatalf("want exactly one of the two overlapping calls to actually run the gate, got %d", ran)
	}
}

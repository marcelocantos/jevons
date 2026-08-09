// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"os"
	"path/filepath"
	"testing"
)

const frontierTestLedger = `
targets:
  T1:
    name: Root achieved
    status: achieved
  T2:
    name: Ready leaf
    status: identified
    acceptance:
    - does the thing
    tags:
    - product
  T3:
    name: Blocked leaf
    status: identified
    depends_on:
    - T2
  T4:
    name: Ready after done dep
    status: converging
    depends_on:
    - T1
  T5:
    name: Unknown dep blocks
    status: identified
    depends_on:
    - T999
  T10:
    name: Natural order after T4
    status: identified
`

// 🎯T254.1: frontier = active targets with all deps done; natural id order.
func TestFrontierLeaves(t *testing.T) {
	leaves, err := FrontierLeaves([]byte(frontierTestLedger))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, l := range leaves {
		ids = append(ids, l.ID)
	}
	want := []string{"T2", "T4", "T10"}
	if len(ids) != len(want) {
		t.Fatalf("leaves = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("leaves = %v, want %v", ids, want)
		}
	}
	if leaves[0].Name != "Ready leaf" || len(leaves[0].Acceptance) != 1 || len(leaves[0].Tags) != 1 {
		t.Fatalf("leaf fields not carried: %+v", leaves[0])
	}
}

func TestLoadFrontierLeavesFromCwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(frontierTestLedger), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	leaves, ledger, err := LoadFrontierLeavesFromCwd(sub)
	if err != nil {
		t.Fatal(err)
	}
	if ledger != filepath.Join(dir, "bullseye.yaml") {
		t.Fatalf("ledger path = %q", ledger)
	}
	if len(leaves) != 3 {
		t.Fatalf("got %d leaves, want 3", len(leaves))
	}
}

func TestTargetIDNaturalLess(t *testing.T) {
	ordered := []string{"T1", "T1.1", "T2", "T10", "T10.2", "T10.10", "T27", "T100"}
	for i := 0; i < len(ordered)-1; i++ {
		if !targetIDNaturalLess(ordered[i], ordered[i+1]) {
			t.Errorf("want %s < %s", ordered[i], ordered[i+1])
		}
		if targetIDNaturalLess(ordered[i+1], ordered[i]) {
			t.Errorf("want NOT %s < %s", ordered[i+1], ordered[i])
		}
	}
}

// 🎯T337: T7→T5 class — active leaf depends on set_aside dep is still a
// frontier leaf (graph-ready) but carries SetAsideDeps for consume park.
const t337SetAsideDepLedger = `
targets:
  T5:
    name: Auth parked
    status: set_aside
    set_aside_reason: parked until iPad resumes
  T6:
    name: Delivered dep
    status: achieved
  T7:
    name: Mobile app for Jevon
    status: converging
    cost: 20
    value: 20
    tags:
    - visual
    depends_on:
    - T5
  T8:
    name: Ready after achieved only
    status: identified
    depends_on:
    - T6
  T9:
    name: Blocked on open dep
    status: identified
    depends_on:
    - T7
`

func TestFrontierLeavesSetAsideDepsCarried(t *testing.T) {
	leaves, err := FrontierLeaves([]byte(t337SetAsideDepLedger))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]FrontierLeaf{}
	for _, l := range leaves {
		byID[l.ID] = l
	}
	// T7 is graph-ready (set_aside unblocks) but must expose SetAsideDeps.
	t7, ok := byID["T7"]
	if !ok {
		t.Fatalf("T7 missing from frontier leaves %v", keysOf(byID))
	}
	if len(t7.SetAsideDeps) != 1 || t7.SetAsideDeps[0] != "T5" {
		t.Fatalf("T7 SetAsideDeps = %v, want [T5]", t7.SetAsideDeps)
	}
	if t7.Cost != 20 || t7.Name != "Mobile app for Jevon" {
		t.Fatalf("T7 fields: cost=%v name=%q", t7.Cost, t7.Name)
	}
	// T8 has only achieved dep — no set_aside deps.
	t8, ok := byID["T8"]
	if !ok {
		t.Fatal("T8 missing")
	}
	if len(t8.SetAsideDeps) != 0 {
		t.Fatalf("T8 SetAsideDeps = %v, want empty", t8.SetAsideDeps)
	}
	// T9 still blocked on open T7.
	if _, ok := byID["T9"]; ok {
		t.Fatal("T9 must not be frontier while T7 is open")
	}
}

func keysOf(m map[string]FrontierLeaf) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestIsSetAsideStatus(t *testing.T) {
	if !IsSetAsideStatus("set_aside") || !IsSetAsideStatus("set-aside") {
		t.Fatal("set_aside variants")
	}
	if IsSetAsideStatus("achieved") || IsSetAsideStatus("identified") {
		t.Fatal("achieved/identified must not count as set_aside")
	}
}

// 🎯T338: T10 parent with active T10.2–T10.6 children carries ActiveChildren;
// ready child leaf has empty ActiveChildren.
const t338ParentChildrenLedger = `
targets:
  T10:
    name: sqlpipe-based state sync
    status: converging
    cost: 13
    value: 20
    context: needs CGO Peer rebuild
  T10.2:
    name: Server Peer + owned tables
    status: identified
    cost: 8
  T10.3:
    name: Client requests path
    status: converging
  T10.6:
    name: Product cutover
    status: identified
    depends_on:
    - T10.2
  T11:
    name: Ordinary ready leaf
    status: identified
  T100:
    name: Unrelated not child of T10
    status: identified
`

func TestFrontierLeavesActiveChildrenCarried(t *testing.T) {
	leaves, err := FrontierLeaves([]byte(t338ParentChildrenLedger))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]FrontierLeaf{}
	for _, l := range leaves {
		byID[l.ID] = l
	}
	t10, ok := byID["T10"]
	if !ok {
		t.Fatalf("T10 missing from frontier leaves %v", keysOf(byID))
	}
	// Active children only (T10.6 depends on open T10.2 so not frontier-ready,
	// but still active hierarchical child of T10).
	wantKids := map[string]bool{"T10.2": true, "T10.3": true, "T10.6": true}
	if len(t10.ActiveChildren) != 3 {
		t.Fatalf("T10 ActiveChildren = %v, want 3 kids", t10.ActiveChildren)
	}
	for _, c := range t10.ActiveChildren {
		if !wantKids[c] {
			t.Fatalf("unexpected child %s in %v", c, t10.ActiveChildren)
		}
	}
	// Ready child leaf T10.2 has no further active descendants.
	t102, ok := byID["T10.2"]
	if !ok {
		t.Fatal("T10.2 must still be a frontier leaf")
	}
	if len(t102.ActiveChildren) != 0 {
		t.Fatalf("T10.2 ActiveChildren = %v, want empty", t102.ActiveChildren)
	}
	// Digit-safe: T100 is not a child of T10.
	for _, c := range t10.ActiveChildren {
		if c == "T100" {
			t.Fatal("T100 must not count as child of T10")
		}
	}
	if _, ok := byID["T11"]; !ok {
		t.Fatal("ordinary ready leaf T11 missing")
	}
}

func TestHierarchicalChildOf(t *testing.T) {
	if !HierarchicalChildOf("T10", "T10.2") || !HierarchicalChildOf("T10", "T10.2.1") {
		t.Fatal("expected hierarchical children")
	}
	if HierarchicalChildOf("T10", "T10") || HierarchicalChildOf("T1", "T10") ||
		HierarchicalChildOf("T10", "T100") || HierarchicalChildOf("T10", "T11") {
		t.Fatal("digit-safe / non-child cases")
	}
}

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

func TestHierarchicalAncestors(t *testing.T) {
	got := HierarchicalAncestors("T254.5.1")
	if len(got) != 2 || got[0] != "T254.5" || got[1] != "T254" {
		t.Fatalf("T254.5.1 ancestors = %v want [T254.5 T254]", got)
	}
	got = HierarchicalAncestors("T10.2")
	if len(got) != 1 || got[0] != "T10" {
		t.Fatalf("T10.2 ancestors = %v want [T10]", got)
	}
	if len(HierarchicalAncestors("T10")) != 0 || len(HierarchicalAncestors("T100")) != 0 {
		t.Fatal("undotted ids have no hierarchical ancestors")
	}
}

func TestIsParkedUmbrellaTag(t *testing.T) {
	if !IsParkedUmbrellaTag([]string{"parked"}) || !IsParkedUmbrellaTag([]string{"fleet", "parked-for-design"}) {
		t.Fatal("parked / parked-for-design must match")
	}
	if IsParkedUmbrellaTag([]string{"design-discussion"}) || IsParkedUmbrellaTag([]string{"needs-owner"}) ||
		IsParkedUmbrellaTag([]string{"owner-parked"}) {
		t.Fatal("design-gated / owner-parked are not parked-umbrella tags")
	}
}

// 🎯T262.5: parked parent + unblocked child carries ParkedAncestors;
// achieved/set_aside parent does not; design-discussion parent does not.
const t2625ParkedUmbrellaLedger = `
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
  T254.5:
    name: recovery
    status: achieved
    tags:
    - parked
  T254.5.9:
    name: grandchild under achieved mid-parent
    status: identified
  T262:
    name: frontier design
    status: converging
    tags:
    - design-discussion
    depends_on:
    - T262.5
  T262.5:
    name: parked umbrella skip
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

func TestFrontierLeavesParkedAncestorsCarried(t *testing.T) {
	leaves, err := FrontierLeaves([]byte(t2625ParkedUmbrellaLedger))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]FrontierLeaf{}
	for _, l := range leaves {
		byID[l.ID] = l
	}
	t2542, ok := byID["T254.2"]
	if !ok {
		t.Fatalf("T254.2 missing from frontier %v", keysOf(byID))
	}
	if len(t2542.ParkedAncestors) != 1 || t2542.ParkedAncestors[0] != "T254" {
		t.Fatalf("T254.2 ParkedAncestors = %v want [T254]", t2542.ParkedAncestors)
	}
	t2543, ok := byID["T254.3"]
	if !ok {
		t.Fatal("T254.3 missing")
	}
	if len(t2543.ParkedAncestors) != 1 || t2543.ParkedAncestors[0] != "T254" {
		t.Fatalf("T254.3 ParkedAncestors = %v want [T254]", t2543.ParkedAncestors)
	}
	// Grandchild: achieved T254.5 does not skip; open parked T254 does.
	t25459, ok := byID["T254.5.9"]
	if !ok {
		t.Fatal("T254.5.9 missing")
	}
	if len(t25459.ParkedAncestors) != 1 || t25459.ParkedAncestors[0] != "T254" {
		t.Fatalf("T254.5.9 ParkedAncestors = %v want [T254] (achieved T254.5 omitted)", t25459.ParkedAncestors)
	}
	// design-discussion parent is not a parked umbrella.
	t2625, ok := byID["T262.5"]
	if !ok {
		t.Fatal("T262.5 missing")
	}
	if len(t2625.ParkedAncestors) != 0 {
		t.Fatalf("T262.5 ParkedAncestors = %v want empty", t2625.ParkedAncestors)
	}
	t1001, ok := byID["T100.1"]
	if !ok {
		t.Fatal("T100.1 missing")
	}
	if len(t1001.ParkedAncestors) != 0 {
		t.Fatalf("achieved parent must not skip: ParkedAncestors = %v", t1001.ParkedAncestors)
	}
	t2001, ok := byID["T200.1"]
	if !ok {
		t.Fatal("T200.1 missing")
	}
	if len(t2001.ParkedAncestors) != 0 {
		t.Fatalf("set_aside parent must not skip: ParkedAncestors = %v", t2001.ParkedAncestors)
	}
	t300, ok := byID["T300"]
	if !ok {
		t.Fatal("T300 missing")
	}
	if len(t300.ParkedAncestors) != 0 {
		t.Fatalf("own parked tag is not an ancestor: ParkedAncestors = %v", t300.ParkedAncestors)
	}
	if _, ok := byID["T500"]; !ok {
		t.Fatal("ordinary T500 missing")
	}
}

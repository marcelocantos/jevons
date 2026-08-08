// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package poproactive

import "testing"

// 🎯T325.1: empty frontier ⇒ sleep, no thrash.
func TestClassifyEmptySleeps(t *testing.T) {
	d := Classify(nil)
	if d.Mode != Sleep || d.Reason != "empty_frontier" {
		t.Fatalf("empty: got mode=%s reason=%s want sleep/empty_frontier", d.Mode, d.Reason)
	}
	d = Classify([]LeafObs{})
	if d.Mode != Sleep || d.Reason != "empty_frontier" {
		t.Fatalf("empty slice: got mode=%s reason=%s", d.Mode, d.Reason)
	}
	if ShouldKeepKicking(nil) {
		t.Fatal("empty must not keep kicking")
	}
}

// 🎯T325.1: ready leaves ⇒ kick (progress; not one-shot strand).
func TestClassifyReadyKicks(t *testing.T) {
	leaves := []LeafObs{
		{ID: "T100", Name: "ship feature"},
		{ID: "T101", Tags: []string{"fleet"}},
	}
	d := Classify(leaves)
	if d.Mode != Kick || d.Reason != "ready_leaves" {
		t.Fatalf("ready: got mode=%s reason=%s want kick/ready_leaves", d.Mode, d.Reason)
	}
	if len(d.ReadyIDs) != 2 {
		t.Fatalf("ready ids: %v", d.ReadyIDs)
	}
	if !ShouldKeepKicking(leaves) {
		t.Fatal("ready must keep kicking")
	}
	// Doctrine aliases
	if ClassifyPOProactive(leaves).Mode != Kick {
		t.Fatal("ClassifyPOProactive alias")
	}
}

// 🎯T325.1: only design-gated / blocked / parked ⇒ sleep (no spawn thrash).
func TestClassifyOnlyGatedSleeps(t *testing.T) {
	leaves := []LeafObs{
		{ID: "T29", Tags: []string{"design-discussion", "needs-owner"}},
		{ID: "T67", Name: "OAuth app pin", Context: "parked-for-design until owner"},
		{ID: "T112", Tags: []string{"design-gated"}},
		{ID: "T200", Blocked: true, Name: "blocked dep"},
	}
	d := Classify(leaves)
	if d.Mode != Sleep || d.Reason != "only_gated_or_engaged" {
		t.Fatalf("gated-only: got mode=%s reason=%s want sleep/only_gated_or_engaged", d.Mode, d.Reason)
	}
	if len(d.ReadyIDs) != 0 {
		t.Fatalf("ready must be empty, got %v", d.ReadyIDs)
	}
}

// 🎯T325.1: already-engaged leaves alone ⇒ sleep (progress in flight, no re-spawn thrash).
func TestClassifyEngagedOnlySleeps(t *testing.T) {
	leaves := []LeafObs{
		{ID: "T50", AlreadyEngaged: true, Name: "in progress worker"},
	}
	d := Classify(leaves)
	if d.Mode != Sleep {
		t.Fatalf("engaged-only must sleep, got %s", d.Mode)
	}
}

// Mixed: one ready among gated ⇒ still kick that ready leaf.
func TestClassifyMixedKeepsReady(t *testing.T) {
	leaves := []LeafObs{
		{ID: "T29", Tags: []string{"needs-owner"}},
		{ID: "T325.1", Name: "PO proactive-until-empty-then-sleep"},
		{ID: "T99", AlreadyEngaged: true},
	}
	d := Classify(leaves)
	if d.Mode != Kick {
		t.Fatalf("mixed with ready must kick, got %s", d.Mode)
	}
	if len(d.ReadyIDs) != 1 || d.ReadyIDs[0] != "T325.1" {
		t.Fatalf("ready ids: %v want [T325.1]", d.ReadyIDs)
	}
}

func TestClassifyLeafPriority(t *testing.T) {
	if k := ClassifyLeaf(LeafObs{ID: "T1", Closed: true}); k != LeafSkipClosed {
		t.Fatalf("closed: %s", k)
	}
	if k := ClassifyFrontierLeaf(LeafObs{ID: "T1", Tags: []string{"parked-for-design"}}); k != LeafSkipDesign {
		t.Fatalf("design: %s", k)
	}
	if k := ClassifyLeaf(LeafObs{ID: "T1", Blocked: true}); k != LeafSkipBlocked {
		t.Fatalf("blocked: %s", k)
	}
	if k := ClassifyLeaf(LeafObs{ID: "T1", AlreadyEngaged: true}); k != LeafSkipEngaged {
		t.Fatalf("engaged: %s", k)
	}
	if k := ClassifyLeaf(LeafObs{ID: "T1"}); k != LeafReady {
		t.Fatalf("ready: %s", k)
	}
}

func TestIsDesignGatedLeafMarkers(t *testing.T) {
	cases := []struct {
		tags    []string
		name    string
		context string
		want    bool
	}{
		{nil, "plain build", "", false},
		{[]string{"fleet", "po"}, "build", "", false},
		{[]string{"needs-owner"}, "", "", true},
		{nil, "", "parked-for-design residual", true},
		{nil, "design-discussion: OAuth", "", true},
		{[]string{"docs-only"}, "README pass", "", true},
		{nil, "T29-class surface", "", true},
		// Bare target ids must not false-gate unrelated leaves.
		{nil, "implement T290 feature", "", false},
		{nil, "T1120 cleanup", "", false},
	}
	for _, tc := range cases {
		got := IsDesignGatedLeaf(tc.tags, tc.name, tc.context)
		if got != tc.want {
			t.Errorf("IsDesignGatedLeaf(%v,%q,%q)=%v want %v", tc.tags, tc.name, tc.context, got, tc.want)
		}
	}
}

// 🎯T325.1 compose T244: sleep + unbound PO + zero children ⇒ not open mission (no thrash).
func TestOpenMissionForProactiveSleepNoThrash(t *testing.T) {
	po := AgentObs{Name: "jevons-po", Purpose: "work"}
	if OpenMissionForProactive(po, Sleep, 0) {
		t.Fatal("sleeping unbound PO with zero kids must not be open mission")
	}
	if POOpenMissionForProactive(po, Kick, 1) == false {
		t.Fatal("PO with work children must stay open mission under kick")
	}
	// Sleep but still has work children ⇒ still open (supervise children).
	if !OpenMissionForProactive(po, Sleep, 2) {
		t.Fatal("sleep with work children still open mission for supervision")
	}
	// Bound target remains open.
	bound := AgentObs{Name: "jevons-po", Purpose: "work", TargetID: "T10"}
	if !OpenMissionForProactive(bound, Sleep, 0) {
		t.Fatal("bound PO stays open mission")
	}
	// Unbound implementer under sleep path stays open.
	worker := AgentObs{Name: "jv-t1-impl", Purpose: "work"}
	if !OpenMissionForProactive(worker, Sleep, 0) {
		t.Fatal("unbound implementer remains open mission")
	}
	// Aside never open mission.
	aside := AgentObs{Name: "aside-1", Purpose: "aside"}
	if OpenMissionForProactive(aside, Kick, 0) {
		t.Fatal("aside must not be open mission")
	}
}

func TestModeString(t *testing.T) {
	if Kick.String() != "kick" || Sleep.String() != "sleep" {
		t.Fatal("mode strings")
	}
}

// 🎯T337: set_aside dep (T7→T5) is not ready for unattended spawn.
func TestClassifyLeafSetAsideDepParks(t *testing.T) {
	o := LeafObs{
		ID: "T7", Name: "Mobile app for Jevon",
		SetAsideDeps: []string{"T5"}, Cost: 20, Tags: []string{"visual"},
	}
	if k := ClassifyLeaf(o); k != LeafSkipSetAsideDep {
		t.Fatalf("set_aside dep: got %s want skip_set_aside_dep", k)
	}
	// force-engage residual overrides.
	o.ForceEngage = true
	if k := ClassifyLeaf(o); k != LeafReady {
		// ForceEngage also clears high-cost mobile; still ready.
		t.Fatalf("force_engage: got %s want ready", k)
	}
	o.ForceEngage = false
	o.Tags = append(o.Tags, "force-engage")
	if k := ClassifyLeaf(o); k != LeafReady {
		t.Fatalf("force-engage tag: got %s want ready", k)
	}
}

// 🎯T337: high-cost mobile without unattended-safe parks; tag overrides.
func TestClassifyLeafHighCostMobile(t *testing.T) {
	// visual + cost≥20
	if k := ClassifyLeaf(LeafObs{ID: "T7", Name: "Some UI", Tags: []string{"visual"}, Cost: 20}); k != LeafSkipHighCostMobile {
		t.Fatalf("visual+cost: got %s", k)
	}
	// name Mobile app
	if k := ClassifyLeaf(LeafObs{ID: "T7", Name: "Mobile app for Jevon", Cost: 5}); k != LeafSkipHighCostMobile {
		t.Fatalf("name mobile: got %s", k)
	}
	// unattended-safe overrides
	if k := ClassifyLeaf(LeafObs{
		ID: "T7", Name: "Mobile app", Tags: []string{"visual", "unattended-safe"}, Cost: 20,
	}); k != LeafReady {
		t.Fatalf("unattended-safe: got %s want ready", k)
	}
	// low-cost visual is fine
	if k := ClassifyLeaf(LeafObs{ID: "T1", Name: "Tidy CSS", Tags: []string{"visual"}, Cost: 3}); k != LeafReady {
		t.Fatalf("low-cost visual: got %s want ready", k)
	}
}

func TestClassifyOnlySetAsideDepSleeps(t *testing.T) {
	d := Classify([]LeafObs{{
		ID: "T7", Name: "Mobile app", SetAsideDeps: []string{"T5"}, Cost: 20, Tags: []string{"visual"},
	}})
	if d.Mode != Sleep || len(d.ReadyIDs) != 0 {
		t.Fatalf("set_aside-only frontier must sleep: %+v", d)
	}
}

// 🎯T338: parent with active children parks; ready child leaf stays ready.
func TestClassifyLeafParentActiveChildren(t *testing.T) {
	parent := LeafObs{
		ID: "T10", Name: "sqlpipe-based state sync",
		ActiveChildren: []string{"T10.2", "T10.3"}, Cost: 13,
	}
	if k := ClassifyLeaf(parent); k != LeafSkipParentActiveChildren {
		t.Fatalf("parent: got %s want skip_parent_with_active_children", k)
	}
	if k := ClassifyLeaf(parent); k.String() != "skip_parent_with_active_children" {
		t.Fatalf("string: %s", k)
	}
	// force-engage residual overrides parent park.
	parent.ForceEngage = true
	if k := ClassifyLeaf(parent); k != LeafReady {
		t.Fatalf("force_engage parent: got %s want ready", k)
	}
	// unattended-safe does NOT override structural parent park.
	parent.ForceEngage = false
	parent.Tags = []string{"unattended-safe"}
	if k := ClassifyLeaf(parent); k != LeafSkipParentActiveChildren {
		t.Fatalf("unattended-safe must not clear parent park: got %s", k)
	}
	// Ready child leaf with no children of its own.
	child := LeafObs{ID: "T10.2", Name: "Server Peer + owned tables", Cost: 8}
	if k := ClassifyLeaf(child); k != LeafReady {
		t.Fatalf("ready child: got %s want ready", k)
	}
}

// 🎯T338: high-infra sqlpipe/CGO/Peer class parks unless unattended-safe.
func TestClassifyLeafHighInfra(t *testing.T) {
	if k := ClassifyLeaf(LeafObs{
		ID: "T10", Name: "sqlpipe-based state sync", Context: "CGO Peer rebuild", Cost: 13,
	}); k != LeafSkipHighInfra {
		t.Fatalf("sqlpipe/cgo: got %s want skip_high_infra", k)
	}
	if k := ClassifyLeaf(LeafObs{
		ID: "T10", Name: "Peer rebuild for sync", Cost: 13,
	}); k != LeafSkipHighInfra {
		t.Fatalf("peer+rebuild cost: got %s", k)
	}
	// unattended-safe overrides high-infra.
	if k := ClassifyLeaf(LeafObs{
		ID: "T10", Name: "sqlpipe sync", Cost: 13, Tags: []string{"unattended-safe"},
	}); k != LeafReady {
		t.Fatalf("unattended-safe: got %s want ready", k)
	}
	// Ordinary low-cost leaf is fine.
	if k := ClassifyLeaf(LeafObs{ID: "T500", Name: "Ordinary ready Build leaf", Cost: 3}); k != LeafReady {
		t.Fatalf("ordinary: got %s want ready", k)
	}
}

func TestClassifyOnlyParentWithChildrenSleeps(t *testing.T) {
	d := Classify([]LeafObs{
		{ID: "T10", Name: "sqlpipe parent", ActiveChildren: []string{"T10.2"}},
	})
	if d.Mode != Sleep || len(d.ReadyIDs) != 0 {
		t.Fatalf("parent-only frontier must sleep: %+v", d)
	}
	d = Classify([]LeafObs{
		{ID: "T10", Name: "sqlpipe parent", ActiveChildren: []string{"T10.2"}},
		{ID: "T10.2", Name: "ready child leaf"},
	})
	if d.Mode != Kick || len(d.ReadyIDs) != 1 || d.ReadyIDs[0] != "T10.2" {
		t.Fatalf("ready child must kick: %+v", d)
	}
}

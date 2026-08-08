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

// 🎯T339: not-urgent / deferred voice class parks; force-engage / unattended-safe override.
func TestClassifyLeafDeferredNotUrgent(t *testing.T) {
	// T22-shaped context: "Not urgent" without design-gated tags.
	t22 := LeafObs{
		ID: "T22", Name: "Voice traffic flows browser-to-Grok directly",
		Context: "Not urgent — laptop dev path works fine via the proxy. Raise once iPad becomes primary.",
	}
	if k := ClassifyLeaf(t22); k != LeafSkipDeferred {
		t.Fatalf("not urgent: got %s want skip_deferred", k)
	}
	if k := ClassifyLeaf(t22); k.String() != "skip_deferred" {
		t.Fatalf("string: %s", k)
	}
	// deferred until / later-device phrase markers.
	if k := ClassifyLeaf(LeafObs{
		ID: "T22b", Name: "Voice residual", Context: "deferred until iPad-in-car; later-device path.",
	}); k != LeafSkipDeferred {
		t.Fatalf("deferred until: got %s", k)
	}
	// Exact tag.
	if k := ClassifyLeaf(LeafObs{ID: "T22c", Name: "Voice", Tags: []string{"not-urgent"}}); k != LeafSkipDeferred {
		t.Fatalf("tag not-urgent: got %s", k)
	}
	// owner-parked tag (must not be swallowed as bare design "parked" only).
	if k := ClassifyLeaf(LeafObs{ID: "T22d", Name: "Voice", Tags: []string{"owner-parked"}}); k != LeafSkipDeferred {
		t.Fatalf("owner-parked tag: got %s want skip_deferred", k)
	}
	if !IsOwnerParkedTag([]string{"owner-parked"}) {
		t.Fatal("IsOwnerParkedTag")
	}
	// force-engage residual.
	t22.ForceEngage = true
	if k := ClassifyLeaf(t22); k != LeafReady {
		t.Fatalf("force_engage: got %s want ready", k)
	}
	// unattended-safe overrides deferred.
	if k := ClassifyLeaf(LeafObs{
		ID: "T22", Name: "Voice", Context: "Not urgent", Tags: []string{"unattended-safe"},
	}); k != LeafReady {
		t.Fatalf("unattended-safe: got %s want ready", k)
	}
	// Ordinary ready leaf without deferred prose stays ready.
	if k := ClassifyLeaf(LeafObs{ID: "T500", Name: "Ordinary ready Build leaf"}); k != LeafReady {
		t.Fatalf("ordinary: got %s want ready", k)
	}
	// Bare residual "deferred" in residual notes alone is not enough (no "deferred until/to").
	if k := ClassifyLeaf(LeafObs{
		ID: "T1", Name: "Some fix", Context: "Residual: telemetry deferred.",
	}); k != LeafReady {
		t.Fatalf("bare residual deferred must not park: got %s", k)
	}
}

func TestClassifyOnlyDeferredSleeps(t *testing.T) {
	d := Classify([]LeafObs{{
		ID: "T22", Name: "Voice", Context: "Not urgent — laptop proxy fine.",
	}})
	if d.Mode != Sleep || len(d.ReadyIDs) != 0 {
		t.Fatalf("deferred-only frontier must sleep: %+v", d)
	}
	d = Classify([]LeafObs{
		{ID: "T22", Name: "Voice", Context: "Not urgent"},
		{ID: "T500", Name: "Ordinary ready leaf"},
	})
	if d.Mode != Kick || len(d.ReadyIDs) != 1 || d.ReadyIDs[0] != "T500" {
		t.Fatalf("ordinary ready must kick: %+v", d)
	}
}

// 🎯T342: T28-shaped car/iPad/VoicelabKit device DSP parks without "not urgent".
// T27.5-shaped hub feed leaf stays ready (must not false-park on bare voice/feed).
func TestClassifyLeafDeviceVoiceDSP(t *testing.T) {
	// Real T28 shape: road-noise / cabin / VoicelabKit / capture pipeline — no "not urgent".
	t28 := LeafObs{
		ID:   "T28",
		Name: "Adaptive road-noise suppression trains on idle cabin audio and subtracts it during speech",
		Context: "The car cabin is the primary deployment and road noise is the dominant threat. " +
			"Slots into the VoicelabKit capture pipeline ahead of the VAD. " +
			"iPad continuously samples cabin/road noise.",
	}
	if !IsDeferredDeviceVoiceLeaf(t28.Tags, t28.Name, t28.Context) {
		t.Fatal("T28-shaped must match IsDeferredDeviceVoiceLeaf")
	}
	if !IsDeferredNotUrgentLeaf(t28.Tags, t28.Name, t28.Context) {
		t.Fatal("T28-shaped must compose into IsDeferredNotUrgentLeaf")
	}
	if k := ClassifyLeaf(t28); k != LeafSkipDeferred {
		t.Fatalf("T28-shaped: got %s want skip_deferred", k)
	}
	// Phrase subsets from acceptance.
	for _, ctx := range []string{
		"road-noise suppressor stage",
		"cabin audio adaptive profile",
		"ipad-in-car primary path",
		"iPad in car road rumble",
		"VoicelabKit capture pipeline stage",
		"voice capture pipeline ahead of VAD",
	} {
		if k := ClassifyLeaf(LeafObs{ID: "T28x", Name: "Device DSP residual", Context: ctx}); k != LeafSkipDeferred {
			t.Fatalf("marker %q: got %s want skip_deferred", ctx, k)
		}
	}
	// Exact device-voice tags.
	if k := ClassifyLeaf(LeafObs{ID: "T28t", Name: "Voice residual", Tags: []string{"skip_device_voice"}}); k != LeafSkipDeferred {
		t.Fatalf("tag skip_device_voice: got %s", k)
	}
	if k := ClassifyLeaf(LeafObs{ID: "T28t2", Name: "Voice residual", Tags: []string{"device-voice"}}); k != LeafSkipDeferred {
		t.Fatalf("tag device-voice: got %s", k)
	}
	// T27.5 hub leaf: provider feeds / aggregation — must still be ready.
	t275 := LeafObs{
		ID:   "T27.5",
		Name: "jevonsd ingests provider data feeds into an aggregated live model broadcast to clients",
		Context: "Feed failures degrade gracefully — a slow or stalled provider surfaces as degraded status " +
			"and never wedges the hub or other providers' feeds. WS fabric broadcast to connected clients.",
		Tags: []string{"providers", "feeds", "aggregation", "streaming"},
	}
	if IsDeferredDeviceVoiceLeaf(t275.Tags, t275.Name, t275.Context) {
		t.Fatal("T27.5 hub must not match device-voice DSP")
	}
	if IsDeferredNotUrgentLeaf(t275.Tags, t275.Name, t275.Context) {
		t.Fatal("T27.5 hub must not be deferred-class")
	}
	if k := ClassifyLeaf(t275); k != LeafReady {
		t.Fatalf("T27.5 hub: got %s want ready", k)
	}
	// unattended-safe / force-engage still open device-voice class (compose T339).
	if k := ClassifyLeaf(LeafObs{
		ID: "T28", Name: t28.Name, Context: t28.Context, ForceEngage: true,
	}); k != LeafReady {
		t.Fatalf("force_engage T28: got %s want ready", k)
	}
	if k := ClassifyLeaf(LeafObs{
		ID: "T28", Name: t28.Name, Context: t28.Context, Tags: []string{"unattended-safe"},
	}); k != LeafReady {
		t.Fatalf("unattended-safe T28: got %s want ready", k)
	}
	// Mixed frontier: only T28 parks; T27.5 kicks.
	d := Classify([]LeafObs{t28, t275})
	if d.Mode != Kick || len(d.ReadyIDs) != 1 || d.ReadyIDs[0] != "T27.5" {
		t.Fatalf("mixed T28 park + T27.5 ready: %+v", d)
	}
}

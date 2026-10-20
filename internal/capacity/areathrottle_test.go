// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"reflect"
	"testing"
)

func TestAreasOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"internal/capacity", "internal/capacity", true},
		{"internal/capacity/", "internal/capacity", true},
		{"internal/capacity", "internal/capacity/spawn.go", true},
		{"internal/capacity/spawn.go", "internal/capacity", true},
		{"internal/capacity", "internal/capacityx", false},
		{"internal/capacity", "internal/cost", false},
		{"", "internal/capacity", false},
	}
	for _, c := range cases {
		if got := areasOverlap(c.a, c.b); got != c.want {
			t.Errorf("areasOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestAdmitByAreaOverlap_UnknownCandidateFallsBackToFlatCap(t *testing.T) {
	d := AdmitByAreaOverlap(TargetFootprint{TargetID: "T1"}, []TargetFootprint{
		{TargetID: "T2", Areas: []string{"internal/cost"}},
	})
	if d.Verdict != AreaThrottleUnknownFootprint {
		t.Fatalf("verdict = %v, want %v", d.Verdict, AreaThrottleUnknownFootprint)
	}
}

func TestAdmitByAreaOverlap_DisjointAdmitsAboveFlatCap(t *testing.T) {
	candidate := TargetFootprint{TargetID: "T1104", Areas: []string{"internal/capacity"}}
	// 50 in-flight workers, all in a disjoint area: a flat cap of (say) 20
	// would refuse this candidate, but disjoint areas must admit anyway.
	var inFlight []TargetFootprint
	for i := 0; i < 50; i++ {
		inFlight = append(inFlight, TargetFootprint{TargetID: "Tbusy", Areas: []string{"internal/cost"}})
	}
	d := AdmitByAreaOverlap(candidate, inFlight)
	if d.Verdict != AreaThrottleAdmitDisjoint {
		t.Fatalf("verdict = %v, want %v (detail=%s)", d.Verdict, AreaThrottleAdmitDisjoint, d.Detail)
	}
	if len(d.ConflictsWith) != 0 {
		t.Fatalf("ConflictsWith = %v, want empty", d.ConflictsWith)
	}
}

func TestAdmitByAreaOverlap_OverlappingSerializes(t *testing.T) {
	candidate := TargetFootprint{TargetID: "T1104", Areas: []string{"internal/capacity/spawn.go"}}
	inFlight := []TargetFootprint{
		{TargetID: "T200", Areas: []string{"internal/cost"}},
		{TargetID: "T201", Areas: []string{"internal/capacity"}},
	}
	d := AdmitByAreaOverlap(candidate, inFlight)
	if d.Verdict != AreaThrottleSerialize {
		t.Fatalf("verdict = %v, want %v", d.Verdict, AreaThrottleSerialize)
	}
	if !reflect.DeepEqual(d.ConflictsWith, []string{"T201"}) {
		t.Fatalf("ConflictsWith = %v, want [T201]", d.ConflictsWith)
	}
}

func TestAdmitByAreaOverlap_UnknownInFlightIgnoredNotBlocking(t *testing.T) {
	candidate := TargetFootprint{TargetID: "T1104", Areas: []string{"internal/capacity"}}
	inFlight := []TargetFootprint{
		{TargetID: "Tmystery"}, // unknown footprint: must not block a disjoint candidate
	}
	d := AdmitByAreaOverlap(candidate, inFlight)
	if d.Verdict != AreaThrottleAdmitDisjoint {
		t.Fatalf("verdict = %v, want %v", d.Verdict, AreaThrottleAdmitDisjoint)
	}
}

func TestFootprintsOverlap_EmptyNeverOverlaps(t *testing.T) {
	if FootprintsOverlap(TargetFootprint{}, TargetFootprint{}) {
		t.Fatal("two empty footprints must not report overlap")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package keepgoing

import "testing"

func TestT566_3ReanimateBeforeNewSpawn(t *testing.T) {
	got := Plan(
		[]string{"T1", "T2"},
		[]Seat{
			{Name: "jv-t1-auto", TargetID: "T1", Running: false},
		},
	)
	if len(got) != 2 {
		t.Fatalf("actions = %#v", got)
	}
	if got[0].Kind != KindReanimate || got[0].TargetID != "T1" || got[0].SeatName != "jv-t1-auto" {
		t.Fatalf("first must remint T1, got %#v", got[0])
	}
	if got[1].Kind != KindSpawn || got[1].TargetID != "T2" {
		t.Fatalf("second must spawn T2, got %#v", got[1])
	}
}

func TestT566_3RunningSeatIsNotRespawned(t *testing.T) {
	got := Plan(
		[]string{"T1", "T2"},
		[]Seat{{Name: "jv-t1-auto", TargetID: "T1", Running: true}},
	)
	if len(got) != 1 || got[0].Kind != KindSpawn || got[0].TargetID != "T2" {
		t.Fatalf("want only T2 spawn, got %#v", got)
	}
}

func TestT566_3AllClearRemintsStoppedSeats(t *testing.T) {
	got := Plan(
		[]string{"T9"},
		[]Seat{{Name: "jv-t9-auto", TargetID: "T9", Running: false}},
	)
	if len(got) != 1 || got[0].Kind != KindReanimate {
		t.Fatalf("all-clear must remint, got %#v", got)
	}
}

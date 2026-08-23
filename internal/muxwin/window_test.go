// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package muxwin

import (
	"reflect"
	"testing"
)

func TestResolveFollowingLastN(t *testing.T) {
	r, err := Resolve(-3, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Following || r.Lo != 8 || r.Hi != 0 {
		t.Fatalf("got %+v want Lo=8 Hi=0 following", r)
	}
}

func TestResolveNegativeRejectedWhenNotFollowing(t *testing.T) {
	if _, err := Resolve(-3, -1, 10); err == nil {
		t.Fatal("expected error for [-3, -1)")
	}
	if _, err := Resolve(-5, 8, 10); err == nil {
		t.Fatal("expected error for negative lo with positive hi")
	}
}

func TestResolveZeroIsEOFNotStart(t *testing.T) {
	if _, err := Resolve(0, 5, 10); err == nil {
		t.Fatal("expected error for lo=0")
	}
}

func TestResolveAbsoluteFrozen(t *testing.T) {
	r, err := Resolve(8, 11, 10)
	if err != nil {
		t.Fatal(err)
	}
	if r.Following || r.Lo != 8 || r.Hi != 11 {
		t.Fatalf("got %+v", r)
	}
}

func TestResolveClampPastEOF(t *testing.T) {
	r, err := Resolve(-100, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if r.Lo != 1 || !r.Following {
		t.Fatalf("got %+v want full follow from 1", r)
	}
}

func TestFreezeFollowingBecomesAbsolute(t *testing.T) {
	r, err := Resolve(-3, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	f := Freeze(r, 10)
	if f.Following || f.Lo != 8 || f.Hi != 11 {
		t.Fatalf("got %+v want [8, 11)", f)
	}
	if Freeze(f, 10) != f {
		t.Fatal("freeze of frozen should be identity")
	}
}

func TestContainsFollowsEOFAndFrozenRange(t *testing.T) {
	follow := Resolved{Lo: 8, Hi: 0, Following: true}
	if !Contains(follow, 8) || !Contains(follow, 115) {
		t.Fatal("following window must include the live end")
	}
	if Contains(follow, 7) || Contains(follow, 0) {
		t.Fatal("following window must not include older than Lo")
	}
	frozen := Resolved{Lo: 8, Hi: 12, Following: false}
	if !Contains(frozen, 8) || !Contains(frozen, 11) {
		t.Fatal("frozen window must include [Lo, Hi)")
	}
	if Contains(frozen, 12) || Contains(frozen, 7) {
		t.Fatal("frozen window must not include Hi or older than Lo")
	}
	if Contains(Resolved{}, 1) {
		t.Fatal("zero window is not a subscription")
	}
}

func TestNeedDeltaOnly(t *testing.T) {
	r := Resolved{Lo: 8, Hi: 0, Following: true}
	have := map[int]struct{}{8: {}, 9: {}}
	got := Need(r, 10, have)
	if !reflect.DeepEqual(got, []int{10}) {
		t.Fatalf("got %v want [10]", got)
	}
}

func TestSubscribeHaloSkipsStepsAndStatus(t *testing.T) {
	// indices 1..8: U S U st U A U A  (S=steps, st=status)
	kinds := []Kind{
		KindUser, KindSteps, KindUser, KindStatus,
		KindUser, KindAssistant, KindUser, KindAssistant,
	}
	// visible last two prose: events 7,8 → [7, 0)
	vis := Resolved{Lo: 7, Hi: 0, Following: true}
	sub := Subscribe(vis, kinds, 2)
	// walk older from 6: 6 assistant (1), 5 user (2) → lo=5. Steps/status not counted.
	if !sub.Following || sub.Lo != 5 || sub.Hi != 0 {
		t.Fatalf("got %+v want Lo=5 Hi=0 following", sub)
	}
}

func TestSubscribeFrozenDilatesBothEdges(t *testing.T) {
	kinds := make([]Kind, 10)
	for i := range kinds {
		kinds[i] = KindUser
	}
	vis := Resolved{Lo: 5, Hi: 7, Following: false} // events 5,6
	sub := Subscribe(vis, kinds, 2)
	if sub.Following || sub.Lo != 3 || sub.Hi != 9 {
		t.Fatalf("got %+v want [3, 9)", sub)
	}
}

func TestCountsTowardHalo(t *testing.T) {
	if !CountsTowardHalo(KindUser) || !CountsTowardHalo(KindAssistant) {
		t.Fatal("prose should count")
	}
	if CountsTowardHalo(KindSteps) || CountsTowardHalo(KindStatus) || CountsTowardHalo(KindOther) {
		t.Fatal("chrome should not count")
	}
}

func TestHaloProseDefault(t *testing.T) {
	if HaloProse != 100 {
		t.Fatalf("HaloProse=%d want 100", HaloProse)
	}
	if HaloMaxExtra != 200 {
		t.Fatalf("HaloMaxExtra=%d want 200", HaloMaxExtra)
	}
}

func TestSubscribeCapsStepSea(t *testing.T) {
	// 500 tools then 2 prose. Visible last 2 following. Uncapped halo
	// would subscribe all 502 events; hydrate would never reach meta.
	kinds := make([]Kind, 502)
	for i := 0; i < 500; i++ {
		kinds[i] = KindSteps
	}
	kinds[500] = KindUser
	kinds[501] = KindAssistant
	vis, err := Resolve(-2, 0, len(kinds))
	if err != nil {
		t.Fatal(err)
	}
	sub := Subscribe(vis, kinds, HaloProse)
	if sub.Lo < len(kinds)-HaloMaxExtra-1 {
		t.Fatalf("sub.Lo=%d walked past HaloMaxExtra (n=%d cap=%d)", sub.Lo, len(kinds), HaloMaxExtra)
	}
	if n := len(Need(sub, len(kinds), nil)); n > HaloMaxExtra+4 {
		t.Fatalf("need=%d too many for hydrate", n)
	}
}

func TestDilateOlderDoesNotPullNewer(t *testing.T) {
	kinds := []Kind{KindUser, KindUser, KindAssistant, KindUser, KindAssistant}
	page := Resolved{Lo: 1, Hi: 3, Following: false}
	got := DilateOlder(page, kinds, 10)
	if got.Hi != 3 || got.Following || got.Lo != 1 {
		t.Fatalf("got %+v want Lo=1 Hi=3 frozen", got)
	}
}

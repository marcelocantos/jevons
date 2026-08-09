// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"testing"
)

// healthy is a snapshot with plenty of room in every dimension.
func healthy() Snapshot {
	return Snapshot{
		Billable:             true,
		Accounting:           "list_price",
		SpentTodayUSD:        50,
		ProjectedTodayUSD:    100,
		DailyBudgetUSD:       500,
		HardCeilingUSDPerDay: 1500,
		TokensUsed:           1_000_000,
		TokensBudget:         20_000_000,
		ActiveSessions:       2,
		MaxSessions:          20,
		ProviderLoad:         map[string]int{"grok": 2},
		ProviderSoftCaps:     map[string]int{"grok": 12},
	}
}

func TestNormalizeClassUnknownRanksLast(t *testing.T) {
	if got := NormalizeClass("some-new-ambient-loop"); got != ClassExperimental {
		t.Fatalf("unknown class = %q, want %q", got, ClassExperimental)
	}
	if Rank(ClassOwnerTurn) >= Rank(ClassResearch) {
		t.Fatalf("owner turn must outrank research")
	}
	if !ClassBuildMission.Foreground() || ClassResearch.Foreground() {
		t.Fatalf("foreground classification wrong")
	}
	// Aliases the loops actually pass.
	for raw, want := range map[string]Class{
		"sentinel": ClassControlRepair, "coach": ClassCoach,
		"research_feed": ClassResearch, "frontier_consume": ClassBuildMission,
	} {
		if got := NormalizeClass(raw); got != want {
			t.Errorf("NormalizeClass(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestHealthyAdmitsEverythingInFull(t *testing.T) {
	snap := healthy()
	a := Assess(snap, DefaultPolicy())
	if a.Pressure != PressureNormal {
		t.Fatalf("pressure = %s, want normal (%+v)", a.Pressure, a)
	}
	for _, c := range Classes() {
		d := Admit(Request{Class: c, Name: "t", Degradable: true}, snap, DefaultPolicy())
		if d.Verdict != VerdictAdmit || d.Tier != TierFull {
			t.Errorf("%s = %s/%s, want admit/full (%s)", c, d.Verdict, d.Tier, d.Detail)
		}
	}
}

func TestCriticalAdmitsOwnerOnly(t *testing.T) {
	snap := healthy()
	snap.SpawnHalted = true // 🎯T36 clamp already fired

	a := Assess(snap, DefaultPolicy())
	if a.Pressure != PressureCritical || !a.OwnerOnly {
		t.Fatalf("pressure = %s owner_only=%v, want critical/true", a.Pressure, a.OwnerOnly)
	}
	if a.Residual == "" {
		t.Error("critical assessment must name its residual")
	}

	if d := Admit(Request{Class: ClassOwnerTurn}, snap, DefaultPolicy()); !d.Admitted() {
		t.Errorf("owner turn refused under critical pressure: %s", d.Detail)
	}
	for _, c := range []Class{ClassControlRepair, ClassAudit, ClassCoach, ClassResearch} {
		d := Admit(Request{Class: c, Degradable: true}, snap, DefaultPolicy())
		if d.Verdict != VerdictDefer || d.Reason != ReasonCriticalOwnerOnly {
			t.Errorf("%s = %s/%s, want defer/%s", c, d.Verdict, d.Reason, ReasonCriticalOwnerOnly)
		}
	}
	// Build work waits for the clamp, but for the clamp's reason — not
	// because ambient policy outranked it.
	d := Admit(Request{Class: ClassBuildMission}, snap, DefaultPolicy())
	if d.Verdict != VerdictDefer || d.Reason != ReasonSpawnHalted {
		t.Errorf("build = %s/%s, want defer/%s", d.Verdict, d.Reason, ReasonSpawnHalted)
	}
}

func TestTightRunsLoadBearingBackgroundOnly(t *testing.T) {
	snap := healthy()
	snap.TokensUsed = 19_000_000 // 5% headroom: inside the 20% owner reserve

	a := Assess(snap, DefaultPolicy())
	if a.Pressure != PressureTight {
		t.Fatalf("pressure = %s, want tight (headroom %.2f)", a.Pressure, a.Headroom)
	}
	if d := Admit(Request{Class: ClassControlRepair}, snap, DefaultPolicy()); !d.Admitted() {
		t.Errorf("load-bearing control repair deferred under tight: %s", d.Detail)
	}
	for _, c := range []Class{ClassStaffOps, ClassAudit, ClassCoach, ClassResearch} {
		d := Admit(Request{Class: c, Degradable: true}, snap, DefaultPolicy())
		if d.Verdict != VerdictDefer || d.Reason != ReasonTightLoadBearing {
			t.Errorf("%s = %s/%s, want defer/%s", c, d.Verdict, d.Reason, ReasonTightLoadBearing)
		}
	}
}

func TestElevatedDegradesAmbientToReducedTier(t *testing.T) {
	snap := healthy()
	snap.TokensUsed = 14_000_000 // 30% headroom: below degrade (40%), above reserve (20%)

	a := Assess(snap, DefaultPolicy())
	if a.Pressure != PressureElevated {
		t.Fatalf("pressure = %s, want elevated (headroom %.2f)", a.Pressure, a.Headroom)
	}
	d := Admit(Request{Class: ClassResearch, Degradable: true}, snap, DefaultPolicy())
	if d.Verdict != VerdictDegrade || d.Tier != TierReduced || d.Reason != ReasonElevatedDegrade {
		t.Errorf("research = %s/%s/%s, want degrade/reduced/%s", d.Verdict, d.Tier, d.Reason, ReasonElevatedDegrade)
	}
	if !d.Admitted() {
		t.Error("a degraded decision must still let the pass run")
	}
	// Work that cannot shrink runs in full rather than pretending.
	if d := Admit(Request{Class: ClassAudit}, snap, DefaultPolicy()); d.Tier != TierFull {
		t.Errorf("non-degradable audit tier = %s, want full", d.Tier)
	}
}

// 🎯T137: under subscription accounting the same USD numbers are estimates,
// so they must not deny background work; tokens and load still can.
func TestSubscriptionAccountingNeverDeniesOnUSD(t *testing.T) {
	snap := healthy()
	snap.SpentTodayUSD, snap.ProjectedTodayUSD = 1400, 2000 // way past both USD lines

	billable := Assess(snap, DefaultPolicy())
	if billable.Pressure != PressureCritical {
		t.Fatalf("billable pressure = %s, want critical", billable.Pressure)
	}

	snap.Billable, snap.Accounting = false, "subscription"
	sub := Assess(snap, DefaultPolicy())
	if sub.Pressure != PressureNormal {
		t.Fatalf("subscription pressure = %s, want normal (USD must not bind)", sub.Pressure)
	}
	if sub.CostHeadroom != unknownHeadroom {
		t.Errorf("subscription cost headroom = %v, want unknown", sub.CostHeadroom)
	}
	if sub.Residual == "" {
		t.Error("subscription assessment must name the USD honesty residual")
	}
	// The honest lever still bites.
	snap.TokensUsed = 19_500_000
	if got := Assess(snap, DefaultPolicy()).Pressure; got != PressureTight {
		t.Errorf("subscription + spent tokens = %s, want tight", got)
	}
}

// 🎯T325.2: one pinned provider is real pressure even when the fleet-wide
// session count looks calm.
func TestProviderSoftCapDrivesLoadHeadroom(t *testing.T) {
	snap := healthy()
	snap.ProviderLoad = map[string]int{"grok": 2, "claude": 8}
	snap.ProviderSoftCaps = map[string]int{"grok": 12, "claude": 8}

	a := Assess(snap, DefaultPolicy())
	if a.LoadHeadroom != 0 {
		t.Fatalf("load headroom = %v, want 0 (claude at soft cap)", a.LoadHeadroom)
	}
	if a.Pressure != PressureCritical {
		t.Fatalf("pressure = %s, want critical", a.Pressure)
	}
}

func TestTokenReserveDefersOversizedBackgroundPass(t *testing.T) {
	snap := healthy()
	snap.TokensBudget, snap.TokensUsed = 1_000_000, 700_000
	// Allowance = 1_000_000 - 700_000 - 20% reserve (200_000) = 100_000.
	pol := DefaultPolicy()

	if d := Admit(Request{Class: ClassResearch, EstTokens: 250_000}, snap, pol); d.Reason != ReasonTokenReserve {
		t.Fatalf("oversized pass = %s/%s, want defer/%s", d.Verdict, d.Reason, ReasonTokenReserve)
	}
	if d := Admit(Request{Class: ClassResearch, EstTokens: 50_000}, snap, pol); !d.Admitted() {
		t.Fatalf("pass inside the allowance deferred: %s", d.Detail)
	}
	// Owner work is never measured against the background allowance.
	if d := Admit(Request{Class: ClassOwnerTurn, EstTokens: 900_000}, snap, pol); !d.Admitted() {
		t.Fatalf("owner turn refused on token reserve: %s", d.Detail)
	}
}

func TestPlanRanksQueueAndFillsSlotsHighestFirst(t *testing.T) {
	snap := healthy()
	pol := DefaultPolicy()
	pol.MaxConcurrentBackground = 2

	// Deliberately out of priority order: the plan must not depend on it.
	queue := []Request{
		{Class: ClassResearch, Name: "research"},
		{Class: ClassOwnerTurn, Name: "owner"},
		{Class: ClassCoach, Name: "coach"},
		{Class: ClassControlRepair, Name: "sentinel"},
		{Class: ClassBuildMission, Name: "build"},
	}
	got := Plan(queue, snap, pol)
	if len(got) != len(queue) {
		t.Fatalf("plan len = %d, want %d", len(got), len(queue))
	}
	for i, d := range got {
		if d.Name != queue[i].Name {
			t.Fatalf("decision %d is %q, want caller order %q", i, d.Name, queue[i].Name)
		}
	}
	byName := map[string]Decision{}
	for _, d := range got {
		byName[d.Name] = d
	}
	for _, n := range []string{"owner", "build", "sentinel", "coach"} {
		if !byName[n].Admitted() {
			t.Errorf("%s not admitted: %s", n, byName[n].Detail)
		}
	}
	// Two background slots went to the two highest-ranked background items.
	if byName["research"].Verdict != VerdictDefer || byName["research"].Reason != ReasonBackgroundSlots {
		t.Errorf("research = %s/%s, want defer/%s", byName["research"].Verdict,
			byName["research"].Reason, ReasonBackgroundSlots)
	}
}

func TestPlanRespectsInFlightPerClassCap(t *testing.T) {
	snap := healthy()
	snap.RunningBackground = map[Class]int{ClassResearch: 1}
	d := Admit(Request{Class: ClassResearch, Name: "second"}, snap, DefaultPolicy())
	if d.Verdict != VerdictDefer || d.Reason != ReasonClassSlots {
		t.Fatalf("second research cycle = %s/%s, want defer/%s", d.Verdict, d.Reason, ReasonClassSlots)
	}
}

// 🎯T291: an owner turn in flight parks ambient work, but not the
// load-bearing repair loop that might be what unsticks the owner's seat.
func TestOwnerTurnInFlightDefersAmbient(t *testing.T) {
	snap := healthy()
	snap.OwnerActive = true
	if d := Admit(Request{Class: ClassCoach}, snap, DefaultPolicy()); d.Reason != ReasonOwnerTurnActive {
		t.Errorf("coach = %s/%s, want defer/%s", d.Verdict, d.Reason, ReasonOwnerTurnActive)
	}
	if d := Admit(Request{Class: ClassControlRepair}, snap, DefaultPolicy()); !d.Admitted() {
		t.Errorf("control repair deferred while owner active: %s", d.Detail)
	}
}

func TestPreemptCancelsLowestRankedPreemptibleFirst(t *testing.T) {
	snap := healthy()
	snap.SpawnHalted = true
	running := []Running{
		{Class: ClassResearch, Name: "research", Preemptible: true},
		{Class: ClassBuildMission, Name: "build", Preemptible: true},
		{Class: ClassCoach, Name: "coach", Preemptible: true},
		{Class: ClassAudit, Name: "audit-midwrite"}, // not preemptible
		{Class: ClassControlRepair, Name: "sentinel", Preemptible: true},
	}
	got := Preempt(running, snap, DefaultPolicy())
	var names []string
	for _, d := range got {
		if d.Verdict != VerdictCancel {
			t.Errorf("%s verdict = %s, want cancel", d.Name, d.Verdict)
		}
		names = append(names, d.Name)
	}
	want := []string{"research", "coach", "sentinel"}
	if len(names) != len(want) {
		t.Fatalf("cancelled %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("cancelled %v, want %v (lowest rank first)", names, want)
		}
	}
}

func TestPreemptSparesLoadBearingWhenMerelyTight(t *testing.T) {
	snap := healthy()
	snap.TokensUsed = 19_000_000 // tight, not critical
	running := []Running{
		{Class: ClassControlRepair, Name: "sentinel", Preemptible: true},
		{Class: ClassResearch, Name: "research", Preemptible: true},
	}
	got := Preempt(running, snap, DefaultPolicy())
	if len(got) != 1 || got[0].Name != "research" {
		t.Fatalf("cancelled %+v, want research only", got)
	}
}

func TestPreemptQuietWhenHealthy(t *testing.T) {
	if got := Preempt([]Running{{Class: ClassResearch, Preemptible: true}}, healthy(), DefaultPolicy()); len(got) != 0 {
		t.Fatalf("healthy preempt = %+v, want none", got)
	}
}

func TestDisabledPolicyAdmitsEverything(t *testing.T) {
	snap := healthy()
	snap.SpawnHalted = true
	pol := DefaultPolicy()
	pol.Disabled = true
	for _, c := range Classes() {
		if d := Admit(Request{Class: c}, snap, pol); !d.Admitted() {
			t.Errorf("%s refused with policy disabled: %s", c, d.Detail)
		}
	}
	if got := Preempt([]Running{{Class: ClassResearch, Preemptible: true}}, snap, pol); len(got) != 0 {
		t.Errorf("disabled policy preempted %+v", got)
	}
}

func TestNoBudgetConfiguredIsSlotBasedOnly(t *testing.T) {
	snap := Snapshot{} // nothing known
	a := Assess(snap, DefaultPolicy())
	if a.Pressure != PressureNormal || a.Headroom != 1 {
		t.Fatalf("empty snapshot = %s/%.2f, want normal/1", a.Pressure, a.Headroom)
	}
	if a.Residual == "" {
		t.Error("an unbounded snapshot must say so rather than imply headroom it never measured")
	}
}

func TestCostAlertLevelRaisesPressure(t *testing.T) {
	for level, want := range map[string]Pressure{
		"": PressureNormal, "none": PressureNormal, "warn": PressureElevated,
		"throttle": PressureTight, "pause": PressureCritical, "kill": PressureCritical,
	} {
		snap := healthy()
		snap.HighestAlert = level
		if got := Assess(snap, DefaultPolicy()).Pressure; got != want {
			t.Errorf("alert %q → %s, want %s", level, got, want)
		}
	}
}

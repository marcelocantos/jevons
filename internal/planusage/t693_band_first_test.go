// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// t693weekly is one published plan weekly at a wall-clock remaining duration.
func t693weekly(provider string, rem, used float64, remain time.Duration, now time.Time) Backend {
	pct := func(v float64) *float64 { return &v }
	resets := now.Add(remain)
	lim := DefaultWeeklyWindowSeconds
	return Backend{
		Provider: provider,
		Status:   StatusAvailable,
		Windows: []Window{{
			Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
			ResetsAt: &resets, LimitWindowSeconds: &lim,
		}},
	}
}

// 🎯T693 owner case: Claude weekly under (~71% used, ~17h to rollover) vs
// Grok weekly ok (~5% used, ~137h left) → pick Claude, even though Grok's
// raw pressure is more negative. Ranking on headroom sent every worker to
// Grok (green over blue).
func TestT693PickPlanDestUnderBeatsOkDespiteGrokSlack(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	claude := t693weekly("claude", 29, 71, 17*time.Hour, now)
	grok := t693weekly("grok", 95, 5, 137*time.Hour, now)

	if got := WeeklyBandOf(claude, now, th); got != BandUnder {
		t.Fatalf("fixture: Claude band=%s, want under", got)
	}
	if got := WeeklyBandOf(grok, now, th); got != BandOK {
		t.Fatalf("fixture: Grok band=%s, want ok", got)
	}

	cands := []DestCand{
		{Provider: "claude", Backend: claude, Load: 8},
		{Provider: "grok", Backend: grok, Load: 1},
	}
	got, ok := PickPlanDest(cands, now, th)
	if !ok || got != "claude" {
		t.Fatalf("under outranks ok: dest=%q ok=%v", got, ok)
	}
	mint := PickMintDest(cands, "grok", now, th)
	if !mint.OK || mint.Provider != "claude" {
		t.Fatalf("mint dest remaining-%% would pick grok (95 vs 29); band-first dest=%+v", mint)
	}

	pick, err := ResolveMint(context.Background(), []DestCand{
		{Provider: "claude", Backend: claude, Load: 8},
		{Provider: "grok", Backend: grok, Load: 1},
	}, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if pick.Provider != claudia.ProviderClaude {
		t.Fatalf("ResolveMint = %q, want claude (production omit-provider path)", pick.Provider)
	}
}

// 🎯T693: Fable weekly_model at 0% does not veto Claude when the plan weekly
// is still eligible. Other models on that provider remain destinations.
func TestT693FableSpentDoesNotVetoClaude(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	pct := func(v float64) *float64 { return &v }
	resets := now.Add(17 * time.Hour)
	lim := DefaultWeeklyWindowSeconds
	claude := Backend{
		Provider: "claude",
		Status:   StatusAvailable,
		Windows: []Window{
			{
				Name: WindowModelWeekly, Model: "Fable",
				RemainingPercent: pct(0), UsedPercent: pct(100),
				ResetsAt: &resets, LimitWindowSeconds: &lim,
			},
			{
				Name: WindowWeekly, RemainingPercent: pct(29), UsedPercent: pct(71),
				ResetsAt: &resets, LimitWindowSeconds: &lim,
			},
			{
				Name: WindowSession, RemainingPercent: pct(80), UsedPercent: pct(20),
			},
		},
	}
	grok := t693weekly("grok", 95, 5, 137*time.Hour, now)

	if !DestEligible(claude, now, th) {
		t.Fatalf("Fable spent must not veto Claude (band=%s)", WeeklyBandOf(claude, now, th))
	}
	if got := WeeklyBandOf(claude, now, th); got != BandUnder {
		t.Fatalf("Claude plan weekly band=%s, want under (not exhausted via Fable)", got)
	}

	got, ok := PickPlanDest([]DestCand{
		{Provider: "claude", Backend: claude, Load: 3},
		{Provider: "grok", Backend: grok, Load: 1},
	}, now, th)
	if !ok || got != "claude" {
		t.Fatalf("Fable spent ≠ Claude unavailable: dest=%q ok=%v", got, ok)
	}

	pick, err := ResolveMint(context.Background(), []DestCand{
		{Provider: "claude", Backend: claude},
		{Provider: "grok", Backend: grok},
	}, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if pick.Provider != claudia.ProviderClaude {
		t.Fatalf("ResolveMint = %q, want claude", pick.Provider)
	}
	if pick.Model == "claude-fable-5" {
		t.Fatalf("ResolveMint landed on spent Fable: %+v", pick)
	}
}

// 🎯T693: locked, then under, then ok. Pressure never promotes ok over under.
func TestT693BandOrderLockedThenUnderThenOK(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	at := func(provider string, rem, used, remTimePct float64) Backend {
		resets := now.Add(time.Duration(remTimePct/100*float64(lim)) * time.Second)
		return Backend{
			Provider: provider, Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
				ResetsAt: &resets, LimitWindowSeconds: &lim,
			}},
		}
	}
	locked := at("codex", 100, 0, 20) // idle, little runway
	under := at("claude", 60, 40, 30)
	ok := at("grok", 50, 50, 50)
	if got := WeeklyBandOf(locked, now, th); got != BandLocked {
		t.Fatalf("fixture locked=%s", got)
	}
	if got := WeeklyBandOf(under, now, th); got != BandUnder {
		t.Fatalf("fixture under=%s", got)
	}
	if got := WeeklyBandOf(ok, now, th); got != BandOK {
		t.Fatalf("fixture ok=%s", got)
	}

	got, yes := PickPlanDest([]DestCand{
		{Provider: "grok", Backend: ok, Load: 0},
		{Provider: "claude", Backend: under, Load: 9},
	}, now, th)
	if !yes || got != "claude" {
		t.Fatalf("under beats ok: dest=%q ok=%v", got, yes)
	}
	got, yes = PickPlanDest([]DestCand{
		{Provider: "claude", Backend: under, Load: 9},
		{Provider: "codex", Backend: locked, Load: 9},
	}, now, th)
	if !yes || got != "codex" {
		t.Fatalf("locked beats under: dest=%q ok=%v", got, yes)
	}
}

func TestT693TightestRemainingIgnoresModelWeekly(t *testing.T) {
	now := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	pct := func(v float64) *float64 { return &v }
	snap := Snapshot{At: now, Backends: []Backend{{
		Provider:    "claude",
		Status:      StatusAvailable,
		FleetAgents: 1,
		Windows: []Window{
			{Name: WindowModelWeekly, Model: "Fable", RemainingPercent: pct(0)},
			{Name: WindowWeekly, RemainingPercent: pct(29)},
			{Name: WindowSession, RemainingPercent: pct(80)},
		},
	}}}
	f, source, ok := snap.TightestRemaining()
	if !ok {
		t.Fatal("plan weekly must still bind capacity")
	}
	if f != 0.29 {
		t.Fatalf("tightest=%v source=%q, want 0.29 from the plan weekly (Fable 0%% is not the provider)", f, source)
	}
	if source != "claude weekly" {
		t.Fatalf("source=%q, want claude weekly", source)
	}
}

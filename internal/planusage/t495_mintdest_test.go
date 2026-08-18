// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"
)

func t495pf(v float64) *float64 { return &v }

// t495Backend publishes a weekly window at a given elapsed point. Pass nil
// used/remaining for a backend that publishes the window but no figures.
func t495Backend(provider string, used, remaining *float64, resetIn time.Duration, now time.Time) Backend {
	reset := now.Add(resetIn)
	lim := DefaultWeeklyWindowSeconds
	return Backend{
		Provider: provider,
		Status:   StatusAvailable,
		Windows: []Window{{
			Name:               WindowWeekly,
			UsedPercent:        used,
			RemainingPercent:   remaining,
			ResetsAt:           &reset,
			LimitWindowSeconds: &lim,
		}},
		FetchedAt: now,
	}
}

func t495Week(frac float64) time.Duration {
	return time.Duration(float64(DefaultWeeklyWindowSeconds) * frac * float64(time.Second))
}

// 🎯T495 oracle 1: grok weekly hot (ineligible) + claude green → the
// omit-provider mint picks claude even when config prefers grok.
func TestT495HotConfigLosesToGreen(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()
	// 50% elapsed: grok 85% used is hot; claude 40% used is green.
	cands := []DestCand{
		{Provider: "grok", Backend: t495Backend("grok", t495pf(85), t495pf(15), t495Week(0.5), now)},
		{Provider: "claude", Backend: t495Backend("claude", t495pf(40), t495pf(60), t495Week(0.5), now)},
	}
	if band := WeeklyBandOf(cands[0].Backend, now, th); band != BandHot {
		t.Fatalf("fixture: grok band=%s want hot", band)
	}
	pick := PickMintDest(cands, "grok", now, th)
	if !pick.OK || pick.Provider != "claude" || pick.ConfigTie {
		t.Fatalf("want claude despite config=grok, got %+v", pick)
	}
}

// 🎯T495 oracle 2: both green with a clear remaining gap → higher
// remaining wins; config preference is ignored.
func TestT495ClearRemainingGapIgnoresConfig(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()
	// 70% elapsed: grok 60% used (ok), claude 15% used (under) — both green.
	cands := []DestCand{
		{Provider: "grok", Backend: t495Backend("grok", t495pf(60), t495pf(40), t495Week(0.3), now)},
		{Provider: "claude", Backend: t495Backend("claude", t495pf(15), t495pf(85), t495Week(0.3), now)},
	}
	for _, c := range cands {
		if !DestEligible(c.Backend, now, th) {
			t.Fatalf("fixture: %s not green (band=%s)", c.Provider, WeeklyBandOf(c.Backend, now, th))
		}
	}
	for _, cfg := range []string{"grok", "claude", ""} {
		pick := PickMintDest(cands, cfg, now, th)
		if !pick.OK || pick.Provider != "claude" || pick.ConfigTie {
			t.Fatalf("cfg=%q: want claude on remaining gap, got %+v", cfg, pick)
		}
	}
}

// 🎯T495 oracle 3: both green with remaining within the indifference
// margin → config preference breaks the tie.
func TestT495IndifferenceTieConfigWins(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()
	// 55% elapsed: 50% vs 52% remaining — no clear winner.
	cands := []DestCand{
		{Provider: "grok", Backend: t495Backend("grok", t495pf(50), t495pf(50), t495Week(0.45), now)},
		{Provider: "claude", Backend: t495Backend("claude", t495pf(48), t495pf(52), t495Week(0.45), now)},
	}
	for _, want := range []string{"grok", "claude"} {
		pick := PickMintDest(cands, want, now, th)
		if !pick.OK || pick.Provider != want || !pick.ConfigTie {
			t.Fatalf("cfg=%q: want config tie-break, got %+v", want, pick)
		}
	}
}

// 🎯T495: a green that publishes no remaining figure is unknown, never 0%
// — it stays in the equally-obvious set and config may still pick it.
func TestT495UnknownRemainingIsNotZero(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()
	cands := []DestCand{
		{Provider: "grok", Backend: t495Backend("grok", t495pf(10), t495pf(90), t495Week(0.5), now)},
		{Provider: "claude", Backend: t495Backend("claude", nil, nil, t495Week(0.5), now)},
	}
	if band := WeeklyBandOf(cands[1].Backend, now, th); band != BandOK {
		t.Fatalf("fixture: unknown-remaining claude band=%s want ok", band)
	}
	pick := PickMintDest(cands, "claude", now, th)
	if !pick.OK || pick.Provider != "claude" || !pick.ConfigTie {
		t.Fatalf("unknown remaining treated as 0: %+v", pick)
	}
	// Without a config preference the known capacity wins outright.
	pick = PickMintDest(cands, "", now, th)
	if !pick.OK || pick.Provider != "grok" || pick.ConfigTie {
		t.Fatalf("no-config outright pick: %+v", pick)
	}
}

// 🎯T495: no green anywhere → refuse; never fall back to an ineligible
// config default.
func TestT495NoGreenRefuses(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()
	cands := []DestCand{
		{Provider: "grok", Backend: t495Backend("grok", t495pf(85), t495pf(15), t495Week(0.5), now)},
		{Provider: "claude", Backend: Backend{Provider: "claude", Status: StatusUnavailable, Reason: "not signed in", FetchedAt: now}},
	}
	pick := PickMintDest(cands, "grok", now, th)
	if pick.OK || pick.Provider != "" {
		t.Fatalf("want refuse with no green, got %+v", pick)
	}
}

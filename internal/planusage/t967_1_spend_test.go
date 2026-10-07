// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// claudeSpendFixture builds the 2026-09-30 live-probe `spend` shape:
// used A$72.84 of A$100, enabled, AUD.
func claudeSpendFixture(enabled bool) *claudia.PlanSpend {
	pct := 72.84
	return &claudia.PlanSpend{
		Enabled: enabled,
		Used:    &claudia.PlanMoney{AmountMinor: 7284, Exponent: 2, Currency: "AUD"},
		Limit:   &claudia.PlanMoney{AmountMinor: 10000, Exponent: 2, Currency: "AUD"},
		Percent: &pct,
	}
}

func claudeBackendAt(sessionUsed float64, weeklyUsed float64, spend *claudia.PlanSpend, now time.Time) Backend {
	b := Backend{
		Provider: "claude",
		Status:   StatusAvailable,
		Windows: []Window{
			bandWindow(WindowSession, sessionUsed, 100-sessionUsed, now, 0.5),
			bandWindow(WindowWeekly, weeklyUsed, 100-weeklyUsed, now, 0.5),
		},
	}
	b.rawSpend = spend
	return b
}

// 🎯T967.1 oracle: the figure appears only while spending — present at
// 100% with spend enabled, absent below 100%, absent with spending
// disabled even at 100%.
func TestT967_1SpendShownOnlyWhileSpending(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	th := DefaultThresholds()

	t.Run("present at 100% with spend enabled", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{claudeBackendAt(100, 8, claudeSpendFixture(true), now)}}
		out := WithBands(snap, now, th)
		sp := out.Backends[0].Spend
		if sp == nil {
			t.Fatal("expected Spend to be served while session window is at 100% and spend enabled")
		}
		if sp.UsedAUD != 72.84 {
			t.Errorf("UsedAUD=%v, want 72.84", sp.UsedAUD)
		}
		if sp.LimitAUD != 100 {
			t.Errorf("LimitAUD=%v, want 100", sp.LimitAUD)
		}
	})

	t.Run("absent below 100% even with spend enabled", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{claudeBackendAt(56, 8, claudeSpendFixture(true), now)}}
		out := WithBands(snap, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("expected no Spend below 100%%, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("absent at 100% with spending disabled", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{claudeBackendAt(100, 8, claudeSpendFixture(false), now)}}
		out := WithBands(snap, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("expected no Spend when spend.enabled=false, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("unavailable with no spend block at all", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{claudeBackendAt(100, 8, nil, now)}}
		out := WithBands(snap, now, th)
		if sp := out.Backends[0].Spend; sp == nil || sp.UnavailableReason == "" || sp.UsedAUD != 0 {
			t.Fatalf("missing spend must say unavailable with a reason, not guess money: %+v", sp)
		}
	})

	t.Run("weekly window at 100% also counts", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{claudeBackendAt(8, 100, claudeSpendFixture(true), now)}}
		out := WithBands(snap, now, th)
		if out.Backends[0].Spend == nil {
			t.Fatal("expected Spend when weekly window (not just session) is at 100%")
		}
	})

	t.Run("per-model weekly window at 100% does not count (🎯T693)", func(t *testing.T) {
		b := claudeBackendAt(8, 8, claudeSpendFixture(true), now)
		b.Windows = append(b.Windows, Window{
			Name: WindowModelWeekly, Model: "Fable",
			UsedPercent: floatPtr(100), RemainingPercent: floatPtr(0),
		})
		out := WithBands(Snapshot{Backends: []Backend{b}}, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("a spent per-model window must not trigger the plan's own spend figure, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("non-AUD currency with no documented rate is unavailable", func(t *testing.T) {
		spend := &claudia.PlanSpend{
			Enabled: true,
			Used:    &claudia.PlanMoney{AmountMinor: 7284, Exponent: 2, Currency: "USD"},
			Limit:   &claudia.PlanMoney{AmountMinor: 10000, Exponent: 2, Currency: "USD"},
		}
		snap := Snapshot{Backends: []Backend{claudeBackendAt(100, 8, spend, now)}}
		out := WithBands(snap, now, th)
		if sp := out.Backends[0].Spend; sp == nil || sp.UnavailableReason == "" {
			t.Fatalf("undocumented currency must say unavailable, not guess AUD: %+v", sp)
		}
	})

	t.Run("missing money at full window reports unavailable", func(t *testing.T) {
		spend := &claudia.PlanSpend{Enabled: true, Limit: &claudia.PlanMoney{AmountMinor: 10000, Exponent: 2, Currency: "AUD"}}
		out := WithBands(Snapshot{Backends: []Backend{claudeBackendAt(100, 8, spend, now)}}, now, th)
		if sp := out.Backends[0].Spend; sp == nil || sp.UnavailableReason == "" || sp.UsedAUD != 0 {
			t.Fatalf("missing money must say unavailable rather than guess: %+v", sp)
		}
	})
	t.Run("spend limit reached is distinct", func(t *testing.T) {
		spend := claudeSpendFixture(true)
		spend.LimitReached = true
		out := WithBands(Snapshot{Backends: []Backend{claudeBackendAt(100, 8, spend, now)}}, now, th)
		if sp := out.Backends[0].Spend; sp == nil || !sp.LimitReached || sp.UsedAUD != 72.84 {
			t.Fatalf("limit reached must retain both amount and flag: %+v", sp)
		}
	})
}

// 🎯T967.1 style, override-proof: a planusage override (🎯T948) that
// forces the bar's Band to BandOK must not touch Spend — the served
// figure is independent of the band, so the cockpit's bold-red styling of
// Spend is never affected by any band override.
func TestT967_1SpendSurvivesOverride(t *testing.T) {
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	th := DefaultThresholds()
	b := claudeBackendAt(100, 8, claudeSpendFixture(true), now)
	b.Override = &Override{Band: BandOK, Reason: "test override"}
	out := WithBands(Snapshot{Backends: []Backend{b}}, now, th)

	if out.Backends[0].Windows[0].Band != string(BandOK) {
		t.Fatalf("expected the override to paint the bar ok, got band %q", out.Backends[0].Windows[0].Band)
	}
	sp := out.Backends[0].Spend
	if sp == nil {
		t.Fatal("expected Spend to still be served under an override forcing the bar green")
	}
	if sp.UsedAUD != 72.84 || sp.LimitAUD != 100 {
		t.Errorf("Spend=%+v, override must not change the figure", sp)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func grokBackendAt(weeklyUsed float64, spend *claudia.PlanSpend, now time.Time) Backend {
	b := Backend{
		Provider: "grok",
		Status:   StatusAvailable,
		Windows: []Window{
			bandWindow(WindowWeekly, weeklyUsed, 100-weeklyUsed, now, 0.5),
		},
	}
	b.rawSpend = spend
	return b
}

func grokOnDemandSpendFixture() *claudia.PlanSpend {
	pct := 32.5
	return &claudia.PlanSpend{
		Enabled: true,
		Used:    &claudia.PlanMoney{AmountMinor: 3250, Exponent: 2, Currency: "AUD"},
		Limit:   &claudia.PlanMoney{AmountMinor: 10000, Exponent: 2, Currency: "AUD"},
		Percent: &pct,
	}
}

// 🎯T967.2 oracle: with on-demand off (no mapped spend), silence everywhere;
// when on-demand is enabled and spending, follow 🎯T967.1 window gating.
func TestT967_2GrokSpendShownOnlyWithOnDemandSpending(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	th := DefaultThresholds()

	t.Run("silent when on-demand disabled (live rollover fixture)", func(t *testing.T) {
		raw, err := os.ReadFile(claudiaGrokFixture("billing_rollover.json"))
		if err != nil {
			t.Fatal(err)
		}
		pu := parseGrokBillingFixture(raw, now)
		if pu.Spend != nil {
			t.Fatalf("rollover fixture must not map Spend when onDemandCap is 0, got %+v", pu.Spend)
		}
		snap := Convert([]claudia.PlanUsage{pu}, nil, now, DefaultStaleAfter)
		out := WithBands(snap, now, th)
		be, ok := out.Backend("grok")
		if !ok {
			t.Fatal("grok backend missing")
		}
		if be.Spend != nil {
			t.Fatalf("expected no Spend with on-demand off, got %+v", be.Spend)
		}
	})

	t.Run("silent below 100% even with on-demand spend mapped", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{grokBackendAt(84, grokOnDemandSpendFixture(), now)}}
		out := WithBands(snap, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("expected no Spend below 100%% weekly, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("present at 100% weekly with on-demand spending", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{grokBackendAt(100, grokOnDemandSpendFixture(), now)}}
		out := WithBands(snap, now, th)
		sp := out.Backends[0].Spend
		if sp == nil {
			t.Fatal("expected Spend at 100% with on-demand spending")
		}
		if sp.UsedAUD != 32.5 || sp.LimitAUD != 100 {
			t.Errorf("Spend=%+v", sp)
		}
	})

	t.Run("cursor still silent with leaked grok spend", func(t *testing.T) {
		b := cursorBackendAt(100, grokOnDemandSpendFixture(), now)
		out := WithBands(Snapshot{Backends: []Backend{b}}, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("cursor must not serve Spend, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("Convert+WithBands from on-demand spending fixture", func(t *testing.T) {
		raw, err := os.ReadFile(claudiaGrokFixture("billing_ondemand_spending.json"))
		if err != nil {
			t.Fatal(err)
		}
		pu := parseGrokBillingFixture(raw, now)
		if pu.Spend == nil || !pu.Spend.Enabled {
			t.Fatalf("fixture must map enabled Spend, got %+v", pu.Spend)
		}
		snap := Convert([]claudia.PlanUsage{pu}, nil, now, DefaultStaleAfter)
		out := WithBands(snap, now, th)
		be, ok := out.Backend("grok")
		if !ok {
			t.Fatal("grok backend missing")
		}
		if be.Spend == nil {
			t.Fatal("expected served Spend at 100% weekly from fixture")
		}
		if be.Spend.UsedAUD != 32.5 || be.Spend.LimitAUD != 100 {
			t.Errorf("Spend=%+v", be.Spend)
		}
	})
}

func parseGrokBillingFixture(raw []byte, now time.Time) claudia.PlanUsage {
	pu, err := claudia.QueryPlanUsage(context.Background(), &claudia.PlanUsageArgs{
		Provider:       claudia.ProviderGrok,
		GrokBillingRaw: raw,
		Now:            now,
	})
	if err != nil {
		panic(err)
	}
	return pu
}

func claudiaGrokFixture(name string) string {
	abs := filepath.Join("/Users/marcelo/work/github.com/marcelocantos/claudia/testdata/grok", name)
	if _, err := os.Stat(abs); err == nil {
		return abs
	}
	return filepath.Join("..", "..", "..", "claudia", "testdata", "grok", name)
}

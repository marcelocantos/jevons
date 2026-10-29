// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// codexOverageSpendFixture mirrors claudia's map from steipete/CodexBar#2736
// (team account, spend_control.individual_limit present).
func codexOverageSpendFixture() *claudia.PlanSpend {
	return &claudia.PlanSpend{
		Enabled: true,
		Used:    &claudia.PlanMoney{AmountMinor: 36, Exponent: 2, Currency: "AUD"},
		Limit:   &claudia.PlanMoney{AmountMinor: 1000, Exponent: 2, Currency: "AUD"},
	}
}

func codexBackendAt(weeklyUsed float64, spend *claudia.PlanSpend, now time.Time) Backend {
	b := Backend{
		Provider: "codex",
		Status:   StatusAvailable,
		Windows: []Window{
			bandWindow(WindowWeekly, weeklyUsed, 100-weeklyUsed, now, 0.5),
		},
	}
	b.rawSpend = spend
	return b
}

// 🎯T967.3 oracle: no credits, balance or dollar figure while overage is off;
// when enabled and the plan window is full, spend follows 🎯T967.1 gating.
func TestT967_3CodexSpendShownOnlyWhileOverageSpending(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	th := DefaultThresholds()

	t.Run("silent when overage disabled (live 2026-10-22 pro probe)", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{codexBackendAt(94, nil, now)}}
		out := WithBands(snap, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("expected no Spend when overage disabled, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("silent below 100% even with overage mapped", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{codexBackendAt(94, codexOverageSpendFixture(), now)}}
		out := WithBands(snap, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("expected no Spend below 100%% window, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("present at 100% with overage enabled and spending", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{codexBackendAt(100, codexOverageSpendFixture(), now)}}
		out := WithBands(snap, now, th)
		sp := out.Backends[0].Spend
		if sp == nil {
			t.Fatal("expected Spend at 100% with overage enabled")
		}
		if sp.UsedAUD != 0.36 || sp.LimitAUD != 10 {
			t.Errorf("Spend=%+v", sp)
		}
	})

	t.Run("cursor still silent with leaked spend", func(t *testing.T) {
		b := cursorBackendAt(100, codexOverageSpendFixture(), now)
		out := WithBands(Snapshot{Backends: []Backend{b}}, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("cursor must not serve Spend, got %+v", out.Backends[0].Spend)
		}
	})
}

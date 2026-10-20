// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// Live probe 2026-10-14 of Cursor DashboardService
// GetCurrentPeriodUsage (the same private surface claudia already polls).
// Body shape observed on the owner's account:
//
//	planUsage.totalSpend / includedSpend / bonusSpend / limit  — unitless
//	  counters (no currency field). bonusSpend is free provider bonus
//	  ("We work with model providers to give you free usage…"), not paid
//	  on-demand overage.
//	spendLimitUsage.limitType = "user"                          — no amount,
//	  no enablement flag, no money block.
//	enabled = true + displayMessage "You've hit your usage limit"
//	  — UI messaging flag, not "on-demand spending is enabled".
//
// Under 🎯T967 (currency + silence rules) and 🎯T967.1 (only while
// spending past the subscription allowance, AUD money only), this is
// not enough to show a dollar figure. Residue on 🎯T967: unit and
// on-demand-enablement signal not established; nothing is shown.
//
// 🎯T967.4 oracle: Cursor never receives a served Spend, even when the
// monthly window is full and even if a PlanSpend somehow leaked onto
// the reading (defence in depth — only Claude is gated through
// spendIfActive).

func cursorBackendAt(monthlyUsed float64, spend *claudia.PlanSpend, now time.Time) Backend {
	lim := int64(30 * 24 * 60 * 60) // Cursor billing cycle ≈ month (🎯T550)
	resets := now.Add(time.Duration((100-monthlyUsed)/100*float64(lim)) * time.Second)
	b := Backend{
		Provider: "cursor",
		Status:   StatusAvailable,
		Windows: []Window{{
			Name:               WindowMonthly,
			UsedPercent:        floatPtr(monthlyUsed),
			RemainingPercent:   floatPtr(100 - monthlyUsed),
			ResetsAt:           &resets,
			LimitWindowSeconds: &lim,
		}},
	}
	b.rawSpend = spend
	return b
}

// cursorSpendShapedLikeClaude is what a mistaken mapper might build from
// planUsage.totalSpend/limit if it treated those unitless counters as
// AUD cents. The live 2026-10-14 numbers were totalSpend=284141,
// limit=40000 — deliberately NOT mapped.
func cursorSpendShapedLikeClaude() *claudia.PlanSpend {
	pct := 100.0
	return &claudia.PlanSpend{
		Enabled: true,
		Used:    &claudia.PlanMoney{AmountMinor: 284141, Exponent: 2, Currency: "AUD"},
		Limit:   &claudia.PlanMoney{AmountMinor: 40000, Exponent: 2, Currency: "AUD"},
		Percent: &pct,
	}
}

func TestT967_4CursorNeverShowsSpend(t *testing.T) {
	now := time.Date(2026, 10, 14, 5, 0, 0, 0, time.UTC)
	th := DefaultThresholds()

	t.Run("no spend on live-shaped reading below 100%", func(t *testing.T) {
		// Live totalPercentUsed was ~91.66%; no rawSpend reaches Convert.
		snap := Snapshot{Backends: []Backend{cursorBackendAt(91.65838709677419, nil, now)}}
		out := WithBands(snap, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("cursor must show no dollar figure below 100%%, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("no spend at 100% monthly with no PlanSpend mapped", func(t *testing.T) {
		snap := Snapshot{Backends: []Backend{cursorBackendAt(100, nil, now)}}
		out := WithBands(snap, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("cursor at full window without a mapped PlanSpend must stay silent, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("no spend even if a PlanSpend leaks onto cursor", func(t *testing.T) {
		// Defence in depth: only Claude is admitted through spendIfActive.
		// A future claudia mapper that stamped PlanSpend onto cursor must
		// still not paint a dollar figure until 🎯T967.4 is deliberately
		// reopened with an established unit + on-demand enablement signal.
		snap := Snapshot{Backends: []Backend{cursorBackendAt(100, cursorSpendShapedLikeClaude(), now)}}
		out := WithBands(snap, now, th)
		if out.Backends[0].Spend != nil {
			t.Fatalf("cursor must not serve Spend even with a leaked PlanSpend, got %+v", out.Backends[0].Spend)
		}
	})

	t.Run("Convert from claudia reading carries no Spend for cursor", func(t *testing.T) {
		// Mirrors what QueryPlanUsage returns today for cursor: windows
		// only, Spend unset. Convert must not invent one.
		used, rem := 100.0, 0.0
		readings := []claudia.PlanUsage{{
			Provider:  claudia.ProviderCursor,
			Status:    claudia.PlanUsageAvailable,
			PlanType:  "pro",
			FetchedAt: now,
			Windows: []claudia.PlanWindow{{
				Name:             claudia.PlanWindowWeekly, // relabelled monthly by Convert
				UsedPercent:      &used,
				RemainingPercent: &rem,
			}},
			// Spend deliberately omitted — live surface has no AUD money block.
		}}
		snap := Convert(readings, nil, now, DefaultStaleAfter)
		out := WithBands(snap, now, th)
		be, ok := out.Backend("cursor")
		if !ok {
			t.Fatal("cursor backend missing after Convert")
		}
		if be.Spend != nil {
			t.Fatalf("Convert+WithBands must leave cursor Spend nil, got %+v", be.Spend)
		}
		if be.rawSpend != nil {
			t.Fatalf("cursor rawSpend must stay nil when claudia published none, got %+v", be.rawSpend)
		}
	})
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"
)

func t39011pct(v float64) *float64 { return &v }

func t39011weeklyAt(now time.Time, rem, used, remTimePct float64) Backend {
	lim := DefaultWeeklyWindowSeconds
	resets := now.Add(time.Duration(remTimePct / 100 * float64(lim) * float64(time.Second)))
	return Backend{
		Provider: "codex",
		Status:   StatusAvailable,
		Windows: []Window{{
			Name: WindowWeekly, RemainingPercent: t39011pct(rem), UsedPercent: t39011pct(used),
			ResetsAt: &resets, LimitWindowSeconds: &lim,
		}},
	}
}

func t39011monthlyAt(now time.Time, rem, used, remTimePct float64) Backend {
	lim := DefaultMonthlyWindowSeconds
	resets := now.Add(time.Duration(remTimePct / 100 * float64(lim) * float64(time.Second)))
	return Backend{
		Provider: "cursor",
		Status:   StatusAvailable,
		Windows: []Window{{
			Name: WindowMonthly, RemainingPercent: t39011pct(rem), UsedPercent: t39011pct(used),
			ResetsAt: &resets, LimitWindowSeconds: &lim,
		}},
	}
}

func t39011sessionAt(now time.Time, rem, used, remTimePct float64) Window {
	lim := DefaultSessionWindowSeconds
	resets := now.Add(time.Duration(remTimePct / 100 * float64(lim) * float64(time.Second)))
	return Window{
		Name: WindowSession, RemainingPercent: t39011pct(rem), UsedPercent: t39011pct(used),
		ResetsAt: &resets, LimitWindowSeconds: &lim,
	}
}

// 🎯T390.1.1: weekly continuation leftover is blue, locked surplus is purple,
// session never paints underutilization, overspend stays orange/red.
func TestT390_1_1WeeklyWasteBands(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	paint := func(be Backend) WeeklyBand {
		return BandOfWindow(be.Windows[0], now, th)
	}

	// Codex 0% used / 19% elapsed is continuation-blue (past 5% warmup).
	if got := paint(t39011weeklyAt(now, 100, 0, 81)); got != BandUnder {
		t.Fatalf("Codex 0%% used / 19%% elapsed → under, got %s", got)
	}

	// First 5% of the window does not flash blue.
	if got := paint(t39011weeklyAt(now, 100, 0, 97)); got != BandOK {
		t.Fatalf("idle week-start inside warmup → ok, got %s", got)
	}

	// Idle Codex does not go purple until ~43% elapsed
	// (locked = 100 − 1.5×time_left ≥ 15 ⇒ time_left ≤ 56.67).
	if got := paint(t39011weeklyAt(now, 100, 0, 60)); got != BandUnder {
		t.Fatalf("idle with 60%% of the week left → under, got %s", got)
	}
	if got := paint(t39011weeklyAt(now, 100, 0, 50)); got != BandLocked {
		t.Fatalf("idle at 50%% elapsed → locked, got %s", got)
	}

	// Owner 2026-09-12: Cursor monthly ~36% remaining / ~9.5% time left is
	// locked surplus (remaining − 1.5×time_left ≈ 22% ≥ 15%), not continuation-blue.
	if got := paint(t39011monthlyAt(now, 36, 64, 9.5)); got != BandLocked {
		t.Fatalf("late-window leftover → locked, got %s", got)
	}

	// Session leftover is not under/locked.
	sess := t39011sessionAt(now, 86, 14, 32)
	if got := BandOfWindow(sess, now, th); got == BandUnder || got == BandLocked {
		t.Fatalf("session must not paint weekly waste, got %s", got)
	}

	// Overspend unchanged; exhausted stays red/exhausted.
	if got := paint(t39011weeklyAt(now, 20, 80, 50)); got != BandHot {
		t.Fatalf("80%% used at 50%% elapsed stays hot, got %s", got)
	}
	if got := paint(t39011weeklyAt(now, 0, 100, 40)); got != BandExhausted {
		t.Fatalf("0 remaining stays exhausted, got %s", got)
	}
}

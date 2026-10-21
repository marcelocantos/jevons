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

// 🎯T390.1.1 as amended by 🎯T1072: weekly/monthly leftover is blue (under)
// or purple (locked) by the Spend Pressure Map, session never paints
// underutilization, overspend stays orange/red.
func TestT390_1_1WeeklyWasteBands(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	paint := func(be Backend) WeeklyBand {
		return BandOfWindow(be.Windows[0], now, th)
	}

	// Codex idle at 19% elapsed: pressure ≈ −0.42, inside ok. The August
	// fixed-15% rule called this blue; the pressure model does not (🎯T1072).
	if got := paint(t39011weeklyAt(now, 100, 0, 81)); got != BandOK {
		t.Fatalf("idle at 19%% elapsed → ok, got %s", got)
	}

	// Idle at 40% elapsed: pressure ≈ −0.92, past the −0.60 under vertex.
	if got := paint(t39011weeklyAt(now, 100, 0, 60)); got != BandUnder {
		t.Fatalf("idle with 60%% of the week left → under, got %s", got)
	}
	// Idle at 50% elapsed: pressure ≈ −1.39, still under (locked is −1.50).
	if got := paint(t39011weeklyAt(now, 100, 0, 50)); got != BandUnder {
		t.Fatalf("idle at 50%% elapsed → under, got %s", got)
	}
	// Idle at 60% elapsed: pressure ≈ −1.83, locked.
	if got := paint(t39011weeklyAt(now, 100, 0, 40)); got != BandLocked {
		t.Fatalf("idle at 60%% elapsed → locked, got %s", got)
	}

	// Owner 2026-09-12: Cursor monthly ~36% remaining / ~9.5% time left is
	// locked surplus, not continuation-blue: pressure ≈ −1.64, past −1.50.
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

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "testing"

// 🎯T591: the daemon owns these vertices, so the margin has to hold here
// too — the cockpit paints from this document.
func TestAheadMarginNeedsRealOverspend(t *testing.T) {
	th := DefaultThresholds()
	if th.AheadMarginPercent != 2 {
		t.Fatalf("default margin = %v, want 2", th.AheadMarginPercent)
	}
	// Damping cannot produce this on its own: for any finite λ,
	// (used+λ)/(elapsed+λ) > 1 whenever used > elapsed. Assert the
	// property rather than trusting the constant.
	for _, lambda := range []float64{0, 5, 9, 100} {
		if got := dampedBurn(1, 0.38, lambda); got <= 1 {
			t.Fatalf("λ=%v damped 1/0.38 to %v — expected still above 1, which is why a margin is needed", lambda, got)
		}
	}
}

// 🎯T595: the daemon's own bands must agree with the cockpit, or mint and
// migrate read a different colour than the owner sees.
func TestWarmupGatesTheBurningFastBands(t *testing.T) {
	th := DefaultThresholds()
	if th.WarmupElapsedPercent != 5 || th.EarlyAlarmUsedPercent != 25 {
		t.Fatalf("defaults moved: warmup=%v early=%v", th.WarmupElapsedPercent, th.EarlyAlarmUsedPercent)
	}
	// The 2026-08-31 live reading: 6% used, 2.13% elapsed.
	if burningFastReachable(6, 2.13, th) {
		t.Fatal("94% remaining at 2% into the week reached a burning-fast band")
	}
	// A real early blowout still does.
	if !burningFastReachable(30, 2, th) {
		t.Fatal("an eighth of the window spent in its first hours was muted")
	}
	// Past warmup, the T591 margin is what decides.
	if !burningFastReachable(9, 5.6, th) {
		t.Fatal("the documented 9%/5.6% vertex stopped being reachable")
	}
	if burningFastReachable(51, 50, th) {
		t.Fatal("1pp of overspend mid-window reached a burning-fast band")
	}
}

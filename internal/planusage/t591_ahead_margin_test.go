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

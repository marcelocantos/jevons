// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"
)

// 🎯T1072: the reading that painted green on 2026-10-14. Cursor had 8.34% of
// the month left and 16h (2.2% of the window) to burn it. The Spend Pressure
// Map reads that as ≈ −1.38, under; the old fixed-15% leftover rule demoted
// it to ok because neither 6.2% continuation nor 5.0% locked reached 15%.
func TestT1072LateMonthCursorLeftoverIsServedUnder(t *testing.T) {
	now := time.Date(2026, 10, 14, 6, 1, 54, 0, time.UTC)
	resets := time.Date(2026, 10, 14, 22, 10, 0, 0, time.UTC)
	lim := DefaultMonthlyWindowSeconds
	rem := 8.34161290322581
	snap := Snapshot{Backends: []Backend{{
		Provider: "cursor",
		Status:   StatusAvailable,
		Windows: []Window{{
			Name: WindowMonthly, RemainingPercent: &rem,
			ResetsAt: &resets, LimitWindowSeconds: &lim,
		}},
	}}}
	th := DefaultThresholds()

	w := WithBands(snap, now, th).Backends[0].Windows[0]
	if w.Band != string(BandUnder) {
		t.Fatalf("served band = %q, want %q", w.Band, BandUnder)
	}
	if w.Pressure == nil || *w.Pressure > th.WasteUnderLn || *w.Pressure <= th.WasteLockedLn {
		t.Fatalf("served pressure = %v, want between waste_locked_ln %v and waste_under_ln %v", w.Pressure, th.WasteLockedLn, th.WasteUnderLn)
	}
}

// The served band is the pressure model's verdict for every leftover case:
// nothing between claudia.ClassifyWindow and the wire re-derives leftover.
// Session is the one exception — a session bar never paints leftover.
func TestT1072ServedLeftoverIsThePressureVerdict(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	for _, rtp := range []float64{97, 81, 60, 50, 40, 20, 9.5, 2.2} {
		for _, used := range []float64{0, 10, 40, 64, 90} {
			be := t39011weeklyAt(now, 100-used, used, rtp)
			w := be.Windows[0]
			want := BandOfPressure(Pressure(used, 100-rtp, th), th)
			if got := BandOfWindow(w, now, th); got != want {
				t.Errorf("weekly used=%v time_left=%v: band %s, pressure verdict %s", used, rtp, got, want)
			}
			sess := t39011sessionAt(now, 100-used, used, rtp)
			if got := BandOfWindow(sess, now, th); got == BandUnder || got == BandLocked {
				t.Errorf("session used=%v time_left=%v painted leftover %s", used, rtp, got)
			}
		}
	}
}

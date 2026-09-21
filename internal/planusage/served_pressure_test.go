// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

// The bar's fill painted red under a served band of "ok": 🎯T610 served the
// verdict but not the number behind it, so the fill kept ramping on the
// used/elapsed ratio. The specimen is the owner's screen on 2026-09-21 —
// claude weekly, 8% used at 3.1% elapsed.
func TestServedPressureIsTheNumberBehindTheBand(t *testing.T) {
	now := time.Date(2026, 9, 21, 7, 17, 0, 0, time.UTC)
	th := DefaultThresholds()
	snap := Snapshot{Backends: []Backend{{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{bandWindow(WindowWeekly, 8, 92, now, 0.969)},
	}}}

	w := WithBands(snap, now, th).Backends[0].Windows[0]
	if w.Band != string(BandOK) {
		t.Fatalf("8%% used at 3.1%% elapsed served as %q, want %q", w.Band, BandOK)
	}
	if w.Pressure == nil {
		t.Fatal("no pressure served beside the band")
	}
	// The control: the superseded ratio really does call this hot, which is
	// what a fill computing it for itself painted.
	if burn := dampedBurn(8, 3.1, th.DampLambdaPercent); burn <= th.HotRatio {
		t.Fatalf("fixture no longer exercises the disagreement: damped burn %.2f", burn)
	}
	// Served pressure and served band are one rule, not two.
	if got := BandOfPressure(*w.Pressure, th); string(got) != w.Band {
		t.Fatalf("BandOfPressure(served pressure %.3f) = %q, served band %q", *w.Pressure, got, w.Band)
	}
	if want := Pressure(8, 3.1, th); math.Abs(*w.Pressure-want) > 1e-6 {
		t.Fatalf("served pressure %.6f, want %.6f", *w.Pressure, want)
	}
	if snap.Backends[0].Windows[0].Pressure != nil {
		t.Fatal("WithBands wrote pressure onto the shared snapshot")
	}
}

// An exhausted window's pressure is +Inf, which JSON cannot carry: the field
// is omitted and the payload still encodes.
func TestServedPressureOmittedWhenNotFinite(t *testing.T) {
	now := time.Date(2026, 9, 21, 7, 17, 0, 0, time.UTC)
	snap := Snapshot{Backends: []Backend{{
		Provider: "claude", Status: StatusAvailable,
		Windows: []Window{bandWindow(WindowWeekly, 100, 0, now, 0.5)},
	}}}

	w := WithBands(snap, now, DefaultThresholds()).Backends[0].Windows[0]
	if w.Pressure != nil {
		t.Fatalf("exhausted window served pressure %v, want none", *w.Pressure)
	}
	raw, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("exhausted window does not encode: %v", err)
	}
	if strings.Contains(string(raw), `"pressure"`) {
		t.Fatalf("pressure key present for an exhausted window: %s", raw)
	}
}

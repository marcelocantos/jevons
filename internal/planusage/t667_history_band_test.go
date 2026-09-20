// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"
)

// 🎯T667: each stored sample carries the band the window had at that
// sample's moment, from the same model as the window's own Band, so the
// sparkline can shift colour along the period.
func TestT667HistorySamplesCarryTheirOwnBand(t *testing.T) {
	th := DefaultThresholds()
	lim := int64(7 * 24 * 3600)
	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	resets := start.Add(time.Duration(lim) * time.Second)
	now := start.Add(6 * 24 * time.Hour) // day 6 of 7

	// A week that started level and then burnt hard: on track at day 1,
	// nearly spent by day 6.
	history := []HistoryPoint{
		{At: start.Add(24 * time.Hour), Remaining: 86},
		{At: start.Add(3 * 24 * time.Hour), Remaining: 55},
		{At: now, Remaining: 3},
	}
	w := Window{
		Name: WindowWeekly, RemainingPercent: pctOf(3), UsedPercent: pctOf(97),
		ResetsAt: &resets, LimitWindowSeconds: &lim, History: history,
	}
	snap := Snapshot{Backends: []Backend{{Provider: "claude", Status: StatusAvailable, Windows: []Window{w}}}}

	got := WithBands(snap, now, th).Backends[0].Windows[0]
	if len(got.History) != len(history) {
		t.Fatalf("history len = %d, want %d", len(got.History), len(history))
	}
	for i, p := range got.History {
		at := w
		rem, used := p.Remaining, 100-p.Remaining
		at.RemainingPercent, at.UsedPercent, at.History = &rem, &used, nil
		if want := string(BandOfWindow(at, p.At, th)); p.Band != want {
			t.Errorf("sample %d at %s: band %q, want the model's %q", i, p.At.Format(time.RFC3339), p.Band, want)
		}
		if p.Band == "" {
			t.Errorf("sample %d has no band", i)
		}
	}
	// The whole point: the colour moves across the period.
	if got.History[0].Band == got.History[len(got.History)-1].Band {
		t.Fatalf("first and last samples share band %q — the fixture no longer shows a shift", got.History[0].Band)
	}
	// The right edge of the sparkline is the bar's colour now.
	if last := got.History[len(got.History)-1].Band; last != got.Band {
		t.Errorf("latest sample band %q != window band %q", last, got.Band)
	}
	// The shared snapshot is not written to.
	if snap.Backends[0].Windows[0].History[0].Band != "" {
		t.Fatal("WithBands stamped the shared snapshot's history in place")
	}
}

// An unavailable backend publishes no verdict for its samples either.
func TestT667UnavailableBackendSamplesHaveNoBand(t *testing.T) {
	lim := int64(7 * 24 * 3600)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	resets := now.Add(24 * time.Hour)
	w := Window{
		Name: WindowWeekly, RemainingPercent: pctOf(50), ResetsAt: &resets, LimitWindowSeconds: &lim,
		History: []HistoryPoint{{At: now.Add(-time.Hour), Remaining: 51}},
	}
	snap := Snapshot{Backends: []Backend{{Provider: "grok", Status: StatusUnavailable, Windows: []Window{w}}}}
	got := WithBands(snap, now, DefaultThresholds()).Backends[0].Windows[0]
	if len(got.History) != 1 || got.History[0].Band != "" {
		t.Fatalf("unavailable backend sample band = %+v", got.History)
	}
}

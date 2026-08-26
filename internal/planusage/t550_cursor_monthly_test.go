// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T550: Cursor's billing-cycle window is monthly on the wire and in the
// cockpit, not mislabeled weekly. Seven-day windows stay weekly.
func TestT550CursorWindowLabeledMonthly(t *testing.T) {
	reset := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	lim := int64(31 * 24 * 3600)
	used := 12.0
	rem := 88.0
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

	snap := Convert([]claudia.PlanUsage{{
		Provider:  claudia.ProviderCursor,
		Status:    claudia.PlanUsageAvailable,
		PlanType:  "ultra",
		FetchedAt: now,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			UsedPercent:      &used,
			RemainingPercent: &rem,
			ResetsAt:         &reset,
			LimitWindow:      time.Duration(lim) * time.Second,
		}},
	}}, nil, now, 0)

	be, ok := snap.Backend("cursor")
	if !ok {
		t.Fatal("cursor backend missing")
	}
	w, ok := be.Window(WindowMonthly)
	if !ok {
		t.Fatalf("want monthly window, got %+v", be.Windows)
	}
	if w.Name != WindowMonthly {
		t.Errorf("name=%q want monthly", w.Name)
	}
	if _, weekly := be.Window(WindowWeekly); weekly {
		t.Error("cursor must not keep a weekly label")
	}
	if w.ResetsAt == nil || !w.ResetsAt.Equal(reset) {
		t.Errorf("resets_at changed: got %v want %v", w.ResetsAt, reset)
	}
	if w.LimitWindowSeconds == nil || *w.LimitWindowSeconds != lim {
		t.Errorf("limit_window_seconds=%v want %d", w.LimitWindowSeconds, lim)
	}

	raw, err := json.Marshal(be)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	wins, _ := wire["windows"].([]any)
	if len(wins) != 1 {
		t.Fatalf("windows=%v", wins)
	}
	win, _ := wins[0].(map[string]any)
	if win["name"] != "monthly" {
		t.Errorf("JSON name=%v want monthly", win["name"])
	}
	if got, _ := win["resets_at"].(string); got == "" {
		t.Errorf("resets_at missing from JSON: %v", win)
	}
}

func TestT550SevenDayWindowStaysWeekly(t *testing.T) {
	reset := time.Now().UTC().Add(3 * 24 * time.Hour)
	lim := int64(7 * 24 * 3600)
	rem := 50.0
	now := time.Now().UTC()

	snap := Convert([]claudia.PlanUsage{{
		Provider:  claudia.ProviderCodex,
		Status:    claudia.PlanUsageAvailable,
		FetchedAt: now,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			RemainingPercent: &rem,
			ResetsAt:         &reset,
			LimitWindow:      time.Duration(lim) * time.Second,
		}},
	}}, nil, now, 0)

	be, _ := snap.Backend("codex")
	if w, ok := be.Window(WindowWeekly); !ok || w.Name != WindowWeekly {
		t.Fatalf("codex 7d must stay weekly, got %+v", be.Windows)
	}
	if _, ok := be.Window(WindowMonthly); ok {
		t.Fatal("7d codex must not become monthly")
	}
}

func TestT550CursorProviderWithoutDurationIsMonthly(t *testing.T) {
	reset := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	rem := 90.0
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

	snap := Convert([]claudia.PlanUsage{{
		Provider:  claudia.ProviderCursor,
		Status:    claudia.PlanUsageAvailable,
		FetchedAt: now,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			RemainingPercent: &rem,
			ResetsAt:         &reset,
		}},
	}}, nil, now, 0)

	be, _ := snap.Backend("cursor")
	if _, ok := be.Window(WindowMonthly); !ok {
		t.Fatalf("cursor without published duration still monthly, got %+v", be.Windows)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func TestAttachCursorAPIUsageFromRawStore(t *testing.T) {
	dir := t.TempDir()
	body := `{"planUsage":{"totalPercentUsed":50.0,"apiPercentUsed":100,"autoPercentUsed":48}}`
	claudia.RecordPlanRawPayload(dir, claudia.ProviderCursor, time.Now(), 200, body)

	used := 50.0
	rem := 50.0
	readings := []claudia.PlanUsage{{
		Provider: claudia.ProviderCursor,
		Status:   claudia.PlanUsageAvailable,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			UsedPercent:      &used,
			RemainingPercent: &rem,
		}},
	}}

	got := attachCursorAPIUsage(readings, dir)
	if len(got) != 1 || len(got[0].Windows) != 2 {
		t.Fatalf("windows=%+v", got)
	}
	api := got[0].Windows[1]
	if api.Name != cursorAPIWindowName || api.Model != "API" || api.UsedPercent == nil || *api.UsedPercent != 100 || api.RemainingPercent == nil || *api.RemainingPercent != 0 {
		t.Fatalf("api window=%+v", api)
	}
	if got[0].Windows[0].Model != "" || got[0].Windows[0].UsedPercent == nil || *got[0].Windows[0].UsedPercent != 50 {
		t.Fatalf("month window changed: %+v", got[0].Windows[0])
	}

	again := attachCursorAPIUsage(got, dir)
	if len(again[0].Windows) != 2 {
		t.Fatalf("second pass duplicated the API window: %+v", again[0].Windows)
	}
}

type monthlyOnlyHistory struct{}

func (monthlyOnlyHistory) Append([]Reading) error { return nil }

func (monthlyOnlyHistory) Series(_, window string, _ *time.Time) ([]HistoryPoint, error) {
	if window == WindowMonthly {
		return []HistoryPoint{{At: time.Unix(10, 0).UTC(), Remaining: 50}}, nil
	}
	return nil, nil
}

func TestCursorAPIHistoryIsNotTheMonth(t *testing.T) {
	dir := t.TempDir()
	claudia.RecordPlanRawPayload(dir, claudia.ProviderCursor, time.Now(), 200, `{"planUsage":{"totalPercentUsed":50,"apiPercentUsed":100}}`)
	used := 50.0
	rem := 50.0
	now := time.Date(2026, 9, 24, 2, 0, 0, 0, time.UTC)
	readings := attachCursorAPIUsage([]claudia.PlanUsage{{
		Provider:  claudia.ProviderCursor,
		Status:    claudia.PlanUsageAvailable,
		FetchedAt: now,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			UsedPercent:      &used,
			RemainingPercent: &rem,
		}},
	}}, dir)
	snap := AttachHistory(Convert(readings, nil, now, 0), monthlyOnlyHistory{})
	be, ok := snap.Backend("cursor")
	if !ok {
		t.Fatal("cursor backend missing")
	}
	var api Window
	found := false
	for _, w := range be.Windows {
		if strings.EqualFold(w.Model, "API") {
			api, found = w, true
		}
	}
	if !found {
		t.Fatal("api window missing")
	}
	if api.Name == WindowMonthly {
		t.Fatalf("api window shares the month name: %+v", api)
	}
	if len(api.History) != 0 {
		t.Fatalf("api sparkline copied the month: %+v", api.History)
	}
	plan, ok := be.PrimaryAllowanceWindow()
	if !ok || len(plan.History) != 1 {
		t.Fatalf("month history = %+v", plan.History)
	}
}

func TestCursorAPIWindowIsSeparatedEvenWhenProducerCopiesMonthlyName(t *testing.T) {
	used, remaining := 50.0, 50.0
	apiUsed, apiRemaining := 100.0, 0.0
	now := time.Date(2026, 9, 24, 2, 0, 0, 0, time.UTC)
	readings := []claudia.PlanUsage{{
		Provider: claudia.ProviderCursor, Status: claudia.PlanUsageAvailable, FetchedAt: now,
		Windows: []claudia.PlanWindow{
			{Name: claudia.PlanWindowWeekly, UsedPercent: &used, RemainingPercent: &remaining},
			{Name: claudia.PlanWindowWeekly, Model: "API", UsedPercent: &apiUsed, RemainingPercent: &apiRemaining},
		},
	}}
	got := Convert(readings, nil, now, 0)
	be, ok := got.Backend("cursor")
	if !ok || len(be.Windows) != 2 || be.Windows[0].Name != WindowMonthly || be.Windows[1].Name != string(cursorAPIWindowName) {
		t.Fatalf("window names must be monthly and api: %+v", be.Windows)
	}
	samples := samplesFromReadings(readings, now)
	if len(samples) != 2 || samples[0].Window != WindowMonthly || samples[0].Remaining != 50 || samples[1].Window != string(cursorAPIWindowName) || samples[1].Remaining != 0 {
		t.Fatalf("history must keep the buckets separate: %+v", samples)
	}
}

func TestAttachCursorAPIUsageSkipsAMissingFigure(t *testing.T) {
	dir := t.TempDir()
	claudia.RecordPlanRawPayload(dir, claudia.ProviderCursor, time.Now(), 200, `{"planUsage":{"totalPercentUsed":50}}`)
	used := 50.0
	readings := []claudia.PlanUsage{{
		Provider: claudia.ProviderCursor,
		Windows:  []claudia.PlanWindow{{Name: claudia.PlanWindowWeekly, UsedPercent: &used}},
	}}
	got := attachCursorAPIUsage(readings, dir)
	if len(got[0].Windows) != 1 {
		t.Fatalf("invented an API window: %+v", got[0].Windows)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

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

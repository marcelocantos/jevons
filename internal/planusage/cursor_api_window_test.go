// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
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
	if api.Model != "API" || api.UsedPercent == nil || *api.UsedPercent != 100 || api.RemainingPercent == nil || *api.RemainingPercent != 0 {
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

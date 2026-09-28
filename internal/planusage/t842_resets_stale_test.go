// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T842: /api/plan-usage must never serve a stale snapshot as current.
//
// A reading can be fresh by fetch age (well under staleAfter) and still
// describe a window whose own resets_at has already passed — the exact
// shape a broker restart produces: the daemon stops polling, comes back up,
// and the first new fetch is timestamped now but the provider's rollover
// clock kept moving underneath it. This test rules out serving that window
// as current.
package planusage_test

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestT842_ResetsAtPassed_MarksStaleEvenWhenFresh(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	resetsAt := now.Add(-1 * time.Minute) // rolled over a minute ago
	fetchedAt := now.Add(-30 * time.Second)

	used := 62.0
	rem := 38.0
	readings := []claudia.PlanUsage{
		{
			Provider:  claudia.ProviderClaude,
			Status:    claudia.PlanUsageAvailable,
			FetchedAt: fetchedAt,
			Windows: []claudia.PlanWindow{
				{
					Name:             claudia.PlanWindowWeekly,
					RemainingPercent: &rem,
					UsedPercent:      &used,
					ResetsAt:         &resetsAt,
				},
			},
		},
	}

	snap := planusage.Convert(readings, nil, now, planusage.DefaultStaleAfter)
	if len(snap.Backends) != 1 {
		t.Fatalf("want 1 backend, got %d", len(snap.Backends))
	}
	be := snap.Backends[0]
	if !be.Stale {
		t.Fatalf("window rolled over 1m ago (fetch only 30s old) must be marked Stale=true, got false (age_seconds=%d)", be.AgeSeconds)
	}

	// CONTROL: same reading but resets_at still in the future must NOT be
	// forced stale — a check that cannot go red here is not evidence.
	futureResets := now.Add(1 * time.Hour)
	readings[0].Windows[0].ResetsAt = &futureResets
	snap2 := planusage.Convert(readings, nil, now, planusage.DefaultStaleAfter)
	if snap2.Backends[0].Stale {
		t.Fatalf("control: resets_at in the future must not be marked stale by this rule")
	}
}

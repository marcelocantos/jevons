// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T689: a reload is not a reason to ask a vendor again.
//
// claudia's request floor waives itself for an explicit refresh, which is
// right for a caller that means it. This daemon is that caller on every
// cockpit mount, so a reload storm stayed a request storm — the shape
// that rate-limited Anthropic's usage endpoint on 2026-09-20 and, through
// 🎯T677, parked a live worker holding 387 queued sends.

func t689Reader(t *testing.T, now *time.Time, calls *atomic.Int64) *Reader {
	t.Helper()
	return NewReader(ReaderArgs{
		Now: func() time.Time { return *now },
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			calls.Add(1)
			return []claudia.PlanUsage{{
				Provider: claudia.ProviderClaude,
				Status:   claudia.PlanUsageAvailable,
				Windows: []claudia.PlanWindow{{
					Name:             claudia.PlanWindowWeekly,
					RemainingPercent: floatPtr(42),
				}},
			}}, nil
		},
	})
}

func TestT689ReloadStormMakesOneRequest(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	r := t689Reader(t, &now, &calls)

	// The first forced refresh is the cockpit's first mount: it fetches.
	if err := r.RefreshNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("first refresh made %d requests, want 1", got)
	}

	// Eight more reloads over the next half minute — the 2026-09-20
	// shape. None of them may reach the vendor.
	for i := 0; i < 8; i++ {
		now = now.Add(4 * time.Second)
		if err := r.RefreshNow(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("a reload storm made %d requests, want 1 — this is the burst that earned a multi-hour refusal", got)
	}

	// The reading is still served, not withheld: a coalesced refresh
	// answers from the snapshot rather than blanking the cockpit.
	snap := r.Snapshot()
	if len(snap.Backends) != 1 || len(snap.Backends[0].Windows) != 1 {
		t.Fatalf("coalescing lost the reading: %+v", snap.Backends)
	}
}

func TestT689RefreshAfterARealGapStillFetches(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	r := t689Reader(t, &now, &calls)
	if err := r.RefreshNow(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Past the floor, a reload is a genuine question again.
	now = now.Add(ForcedRefreshFloor + time.Second)
	if err := r.RefreshNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("a refresh past the floor made %d requests, want 2 — a reload after a real gap must be live", got)
	}
}

func TestT689TheBackgroundPollIsUnaffected(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	r := t689Reader(t, &now, &calls)

	// The scheduled poll is not a forced refresh and keeps its own
	// cadence: the floor governs reloads, not the daemon's own clock.
	for i := 0; i < 3; i++ {
		if err := r.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("background polls made %d requests, want 3", got)
	}
}

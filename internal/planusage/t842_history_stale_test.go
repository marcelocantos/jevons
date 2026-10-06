// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T842 (remaining acceptance): a window's remaining_percent/band is never
// older than the newest entry in its own history array, and a stale reading
// — whether staled by resets_at or by this history check — must not be
// misread as a capacity restoration (🎯T977) that wakes a fleet standing
// down for capacity.
package planusage_test

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// TestT842_HistoryNewerThanLiveReading_MarksStale covers the acceptance's
// named shape directly: the live source drops out after a fresh reading —
// the broker keeps echoing an earlier cached payload under a fresh-looking
// FetchedAt, while the readings store already holds a newer sample for the
// same period. resets_at alone (🎯T842's first half) cannot see this: both
// readings share the same still-future resets_at.
func TestT842_HistoryNewerThanLiveReading_MarksStale(t *testing.T) {
	store, err := planusage.OpenReadingStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	resetsAt := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	stuckAt := time.Date(2026, 9, 22, 0, 28, 0, 0, time.UTC) // the live source's last real fetch, 32%
	newerSample := stuckAt.Add(8 * time.Minute)              // 00:36Z, 31% — someone else's poll landed
	now := stuckAt.Add(10 * time.Minute)                     // comfortably inside staleAfter and before resets_at

	// The store already holds a sample newer than the live source's last
	// real fetch — e.g. another process's poll landed while this broker's
	// live source had already stalled.
	if err := store.Append([]planusage.Reading{
		{Provider: "claude", Window: "weekly", FetchedAt: stuckAt, Remaining: 32, ResetsAt: &resetsAt},
		{Provider: "claude", Window: "weekly", FetchedAt: newerSample, Remaining: 31, ResetsAt: &resetsAt},
	}); err != nil {
		t.Fatal(err)
	}

	// The endpoint is asked for the current snapshot (`now`) and the live
	// source is still echoing its last real fetch (`stuckAt`, 10 minutes
	// old — nowhere near staleAfter, and resets_at still in the future):
	// the claudia v0.42.0 broker-restart specimen exactly. Age-based and
	// resets_at-based staleness both pass; only comparing against the
	// window's own history catches it.
	readings := []claudia.PlanUsage{t842reading(32, resetsAt, stuckAt)}
	snap := planusage.AttachHistory(planusage.Convert(readings, nil, now, planusage.DefaultStaleAfter), store)
	be, ok := snap.Backend("claude")
	if !ok {
		t.Fatalf("no claude backend in %+v", snap)
	}
	if !be.Stale {
		t.Fatalf("a live reading older than its own window's newest history sample must be Stale=true, got false (fetched_at=%s)", be.FetchedAt)
	}

	// CONTROL: a live reading that IS at least as new as the newest stored
	// sample (the 31% the store already has at `newerSample`) is not
	// forced stale by this rule — a check that cannot go green here is
	// not evidence either.
	readings2 := []claudia.PlanUsage{t842reading(31, resetsAt, newerSample)}
	snap2 := planusage.AttachHistory(planusage.Convert(readings2, nil, now, planusage.DefaultStaleAfter), store)
	be2, ok := snap2.Backend("claude")
	if !ok {
		t.Fatalf("no claude backend in %+v", snap2)
	}
	if be2.Stale {
		t.Fatalf("control: a reading at least as new as its own history must not be forced stale by this rule")
	}
}

func t842reading(remaining float64, resetsAt, fetchedAt time.Time) claudia.PlanUsage {
	rem := remaining
	used := 100 - remaining
	return claudia.PlanUsage{
		Provider:  claudia.ProviderClaude,
		Status:    claudia.PlanUsageAvailable,
		FetchedAt: fetchedAt,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			RemainingPercent: &rem,
			UsedPercent:      &used,
			ResetsAt:         &resetsAt,
			LimitWindow:      7 * 24 * time.Hour,
		}},
	}
}

// TestT842_StaleReading_DoesNotUnparkOrAnnounceRestoration covers the
// acceptance's "the plan policy that parks or unparks seats consumes the
// same staleness marker and does not unpark on a snapshot older than its
// own last decision" by exercising the two policy entry points that read a
// Backend's live band off a Snapshot: resolveSeatPlacement (via
// PlanDecisions, which decides stay/park/migrate) and CapacityWatch (🎯T977,
// which announces a restoration). Both must treat Stale as unknown — the
// same T677 treatment already given to an unreadable backend — not as
// grounds to unpark/park or to announce a restoration.
func TestT842_StaleReading_DoesNotUnparkOrAnnounceRestoration(t *testing.T) {
	now := time.Date(2026, 9, 22, 10, 41, 0, 0, time.UTC)
	th := planusage.DefaultThresholds()

	// The specimen shape: weekly reads "ahead" (would normally read as
	// admissible/not-vacating under the old band), but it is Stale.
	aheadButStale := planusage.Backend{
		Provider: "claude", Status: planusage.StatusAvailable, Stale: true,
		Windows: []planusage.Window{bandWindow842(now)},
	}

	// Policy: a parked seat recovering must not be told to resume/migrate
	// on the strength of a stale reading alone.
	agent := planusage.AgentRef{Name: "jv-stale-recover", Provider: "claude", RecoverPlanPark: true}
	decisions := planusage.PlanDecisions(
		planusage.Snapshot{Backends: []planusage.Backend{aheadButStale}},
		[]planusage.AgentRef{agent}, now, th,
	)
	if len(decisions) != 1 {
		t.Fatalf("want 1 decision, got %d", len(decisions))
	}
	if decisions[0].Action == planusage.SeatMigrate && decisions[0].To == "claude" {
		t.Fatalf("a parked seat must not be resumed on its own provider off a stale reading, got %+v", decisions[0])
	}

	// CapacityWatch: a seat that stood down because this provider read
	// inadmissible must not see a stale "ahead" reading as restoration.
	var w planusage.CapacityWatch
	exhausted := aheadButStale
	exhausted.Stale = false
	if restored := w.Observe(planusage.Snapshot{Backends: []planusage.Backend{exhausted}}, now, th); len(restored) != 0 {
		t.Fatalf("fixture: first observe of an inadmissible plan must not announce, got %v", restored)
	}
	if restored := w.Observe(planusage.Snapshot{Backends: []planusage.Backend{aheadButStale}}, now, th); len(restored) != 0 {
		t.Fatalf("a stale reading must not be announced as a capacity restoration, got %v", restored)
	}
}

// bandWindow842 is a weekly window reading 92% used / 8% remaining with
// resets_at comfortably in the future — "ahead" territory, the band that
// would read as admissible/vacate-worthy if Stale were ignored.
func bandWindow842(now time.Time) planusage.Window {
	resets := now.Add(24 * time.Hour)
	used := 92.0
	rem := 8.0
	return planusage.Window{
		Name:             planusage.WindowWeekly,
		UsedPercent:      &used,
		RemainingPercent: &rem,
		ResetsAt:         &resets,
		Band:             string(planusage.BandAhead),
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func t634pct(v float64) *float64 { return &v }

func t634reset(t time.Time) *time.Time { return &t }

func t634reading(provider claudia.Provider, remaining float64, reset time.Time, at time.Time) claudia.PlanUsage {
	return claudia.PlanUsage{
		Provider:  provider,
		Status:    claudia.PlanUsageAvailable,
		FetchedAt: at,
		Windows: []claudia.PlanWindow{{
			Name:             claudia.PlanWindowWeekly,
			RemainingPercent: t634pct(remaining),
			ResetsAt:         t634reset(reset),
			LimitWindow:      7 * 24 * time.Hour,
		}},
	}
}

func TestT634RefreshAppendsOnlyOnSuccess(t *testing.T) {
	store, err := planusage.OpenReadingStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	reset := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	ok := planusage.NewReader(planusage.ReaderArgs{
		Now:     func() time.Time { return at },
		History: store,
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return []claudia.PlanUsage{t634reading(claudia.ProviderClaude, 62, reset, at)}, nil
		},
	})
	if err := ok.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	pts, err := store.Series("claude", "weekly", &reset)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].Remaining != 62 {
		t.Fatalf("success series=%v", pts)
	}

	fail := planusage.NewReader(planusage.ReaderArgs{
		History: store,
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return nil, errors.New("provider down")
		},
	})
	if err := fail.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	pts, err = store.Series("claude", "weekly", &reset)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 {
		t.Fatalf("failed refresh must not append, got %d", len(pts))
	}
}

func TestT634RolloverStartsANewSeries(t *testing.T) {
	store, err := planusage.OpenReadingStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	oldReset := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	newReset := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	var n int
	r := planusage.NewReader(planusage.ReaderArgs{
		History: store,
		Now:     func() time.Time { return t1 },
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			n++
			if n == 1 {
				return []claudia.PlanUsage{t634reading(claudia.ProviderClaude, 40, oldReset, t0)}, nil
			}
			return []claudia.PlanUsage{t634reading(claudia.ProviderClaude, 95, newReset, t1)}, nil
		},
	})
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	old, err := store.Series("claude", "weekly", &oldReset)
	if err != nil {
		t.Fatal(err)
	}
	cur, err := store.Series("claude", "weekly", &newReset)
	if err != nil {
		t.Fatal(err)
	}
	if len(old) != 1 || old[0].Remaining != 40 {
		t.Fatalf("old series=%v", old)
	}
	if len(cur) != 1 || cur[0].Remaining != 95 {
		t.Fatalf("new series=%v", cur)
	}

	snap := r.Snapshot()
	be, ok := snap.Backend("claude")
	if !ok {
		t.Fatal("missing claude")
	}
	w, ok := be.Window("weekly")
	if !ok {
		t.Fatal("missing weekly")
	}
	if len(w.History) != 1 || w.History[0].Remaining != 95 {
		t.Fatalf("snapshot must attach only the current period, got %v", w.History)
	}
}

func TestT634HistoryStartsAtFirstSample(t *testing.T) {
	store, err := planusage.OpenReadingStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	reset := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
	r := planusage.NewReader(planusage.ReaderArgs{
		History: store,
		Now:     func() time.Time { return at },
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return []claudia.PlanUsage{t634reading(claudia.ProviderClaude, 71, reset, at)}, nil
		},
	})
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	w, _ := r.Snapshot().Backend("claude")
	win, _ := w.Window("weekly")
	if len(win.History) != 1 {
		t.Fatalf("history=%v", win.History)
	}
	if !win.History[0].At.Equal(at) || win.History[0].Remaining != 71 {
		t.Fatalf("must start at first sample, not a fabricated 100%%: %v", win.History[0])
	}
}

func TestT634EmptyHistoryWhenStoreEmpty(t *testing.T) {
	store, err := planusage.OpenReadingStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	reset := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
	r := planusage.NewReader(planusage.ReaderArgs{
		History: store,
		Now:     func() time.Time { return at },
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return []claudia.PlanUsage{t634reading(claudia.ProviderClaude, 50, reset, at)}, nil
		},
	})
	// Snapshot before any successful Refresh — store empty, no invented curve.
	be, ok := r.Snapshot().Backend("claude")
	if ok {
		if w, found := be.Window("weekly"); found && len(w.History) != 0 {
			t.Fatalf("empty store must not invent history: %v", w.History)
		}
	}
}

func TestT634IsolateStoreDoesNotReadDevelopmentPath(t *testing.T) {
	devDir := t.TempDir()
	isoDir := t.TempDir()
	dev, err := planusage.OpenReadingStore(planusage.DefaultReadingsPath(devDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	iso, err := planusage.OpenReadingStore(planusage.DefaultReadingsPath(isoDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = iso.Close() })

	if filepath.Base(planusage.DefaultReadingsPath(devDir)) == "usage.db" {
		t.Fatal("readings must not live in usage.db")
	}

	reset := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	if err := dev.Append([]planusage.Reading{{
		Provider: "claude", Window: "weekly", FetchedAt: at, Remaining: 12, ResetsAt: &reset,
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := iso.Series("claude", "weekly", &reset)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("isolate store saw development readings: %v", got)
	}
}

func TestT634FixtureSnapshotDoesNotReadStore(t *testing.T) {
	dir := t.TempDir()
	store, err := planusage.OpenReadingStore(planusage.DefaultReadingsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	reset := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	if err := store.Append([]planusage.Reading{{
		Provider: "claude", Window: "weekly", FetchedAt: time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
		Remaining: 12, ResetsAt: &reset,
	}}); err != nil {
		t.Fatal(err)
	}

	fix := filepath.Join(dir, "fixture.json")
	body := `{"backends":[{"provider":"claude","status":"available","windows":[{"name":"weekly","remaining_percent":50,"resets_at":"2026-09-14T00:00:00Z","history":[{"at":"2026-09-08T11:00:00Z","remaining_percent":77}]}]}]}`
	if err := os.WriteFile(fix, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	r := planusage.NewReader(planusage.ReaderArgs{
		History:     store,
		FixturePath: fix,
	})
	w, ok := r.Snapshot().Backend("claude")
	if !ok {
		t.Fatal("missing claude")
	}
	win, ok := w.Window("weekly")
	if !ok || len(win.History) != 1 || win.History[0].Remaining != 77 {
		t.Fatalf("fixture history must pass through unchanged, got %+v", win)
	}
}

func TestT634DownsampleKeepsEndsAndDoesNotInvent(t *testing.T) {
	var pts []planusage.HistoryPoint
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 200; i++ {
		pts = append(pts, planusage.HistoryPoint{
			At:        start.Add(time.Duration(i) * time.Minute),
			Remaining: float64(200 - i),
		})
	}
	got := planusage.Downsample(pts, 10)
	if len(got) > 10 {
		t.Fatalf("len=%d want ≤10", len(got))
	}
	if !got[0].At.Equal(pts[0].At) || got[0].Remaining != pts[0].Remaining {
		t.Fatalf("first=%v want %v", got[0], pts[0])
	}
	last := pts[len(pts)-1]
	if !got[len(got)-1].At.Equal(last.At) || got[len(got)-1].Remaining != last.Remaining {
		t.Fatalf("last=%v want %v", got[len(got)-1], last)
	}
	if got[0].Remaining == 100 && !got[0].At.Equal(pts[0].At) {
		t.Fatal("downsample invented a 100% start")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestT879OpeningStoreRepairsOnlyContradictoryCursorMonthlyZeros(t *testing.T) {
	path := planusage.DefaultReadingsPath(t.TempDir())
	store, err := planusage.OpenReadingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	reset := time.Date(2026, 10, 14, 22, 10, 0, 0, time.UTC)
	misfiledAt := time.Date(2026, 9, 23, 15, 54, 12, 0, time.UTC)
	realZeroAt := misfiledAt.Add(time.Hour)
	if err := store.Append([]planusage.Reading{
		{Provider: "cursor", Window: "monthly", FetchedAt: misfiledAt, ResetsAt: &reset, Remaining: 49.952},
		{Provider: "cursor", Window: "monthly", FetchedAt: misfiledAt, ResetsAt: &reset, Remaining: 0},
		{Provider: "cursor", Window: "monthly", FetchedAt: realZeroAt, ResetsAt: &reset, Remaining: 0},
		{Provider: "claude", Window: "monthly", FetchedAt: misfiledAt, ResetsAt: &reset, Remaining: 0},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		store, err = planusage.OpenReadingStore(path)
		if err != nil {
			t.Fatal(err)
		}
		cursor, err := store.Series("cursor", "monthly", &reset)
		if err != nil {
			t.Fatal(err)
		}
		if len(cursor) != 2 || cursor[0].Remaining != 49.952 || cursor[1].Remaining != 0 || !cursor[1].At.Equal(realZeroAt) {
			t.Fatalf("open %d cursor monthly series=%+v", i, cursor)
		}
		claude, err := store.Series("claude", "monthly", &reset)
		if err != nil {
			t.Fatal(err)
		}
		if len(claude) != 1 || claude[0].Remaining != 0 {
			t.Fatalf("open %d other provider series=%+v", i, claude)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
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

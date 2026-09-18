// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"path/filepath"
	"testing"
	"time"
)

// The real shape, from ~/.jevons/plan-usage-readings.db on 2026-09-18: one
// Claude week arrived under four rollover timestamps two seconds apart, and
// the 475/395/177/4 samples it held were four series instead of one.
var t669Jitter = []string{
	"2026-09-21T01:59:58Z",
	"2026-09-21T01:59:59Z",
	"2026-09-21T02:00:00Z",
	"2026-09-21T02:00:01Z",
}

func t669Store(t *testing.T) *ReadingStore {
	t.Helper()
	st, err := OpenReadingStore(filepath.Join(t.TempDir(), "readings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func t669At(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// 🎯T669: rollover jitter does not split a period into several series.
func TestT669JitteringRolloverIsOneSeries(t *testing.T) {
	st := t669Store(t)
	base := t669At(t, "2026-09-16T00:00:00Z")
	for i, key := range t669Jitter {
		resets := t669At(t, key)
		if err := st.Append([]Reading{{
			Provider: "claude", Window: WindowWeekly,
			FetchedAt: base.Add(time.Duration(i) * time.Hour),
			Remaining: float64(90 - i), ResetsAt: &resets,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	// Every published spelling of that rollover answers with the whole week.
	for _, key := range t669Jitter {
		resets := t669At(t, key)
		pts, err := st.Series("claude", WindowWeekly, &resets)
		if err != nil {
			t.Fatal(err)
		}
		if len(pts) != len(t669Jitter) {
			t.Fatalf("Series(%s) = %d samples, want all %d", key, len(pts), len(t669Jitter))
		}
		if pts[0].Remaining != 90 {
			t.Fatalf("Series(%s) starts at %v, want the earliest sample (90)", key, pts[0].Remaining)
		}
	}
	// A genuinely different period is still its own series.
	other := t669At(t, "2026-09-14T02:00:00Z")
	pts, err := st.Series("claude", WindowWeekly, &other)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 0 {
		t.Fatalf("last week's key returned %d samples from this week", len(pts))
	}
}

// Rows written before the bucketing rejoin their series when the store opens.
func TestT669ExistingSplitRowsAreRebucketedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readings.db")
	st, err := OpenReadingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	// Write the pre-🎯T669 shape directly: one row per jittered key.
	base := t669At(t, "2026-09-16T00:00:00Z")
	for i, key := range t669Jitter {
		if _, err := st.db.Exec(
			`INSERT INTO plan_readings(provider, window, resets_key, fetched_at, remaining) VALUES (?,?,?,?,?)`,
			"claude", WindowWeekly, key, base.Add(time.Duration(i)*time.Hour).Format(time.RFC3339), float64(90-i),
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenReadingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resets := t669At(t, "2026-09-21T02:00:00Z")
	pts, err := reopened.Series("claude", WindowWeekly, &resets)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != len(t669Jitter) {
		t.Fatalf("after rebucketing, Series = %d samples, want %d", len(pts), len(t669Jitter))
	}
	// Idempotent: opening again neither loses nor duplicates rows.
	again, err := OpenReadingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	pts2, err := again.Series("claude", WindowWeekly, &resets)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts2) != len(pts) {
		t.Fatalf("second open changed the series: %d → %d", len(pts), len(pts2))
	}
}

// The bucket is wide enough for observed jitter and far narrower than the
// gap between two periods, so it can never merge different ones.
func TestT669BucketIsWiderThanJitterAndNarrowerThanAPeriod(t *testing.T) {
	if ResetsKeyBucket < time.Minute {
		t.Fatalf("bucket %s is too narrow for second-level jitter", ResetsKeyBucket)
	}
	if ResetsKeyBucket >= time.Hour {
		t.Fatalf("bucket %s could merge two short (5h session) periods", ResetsKeyBucket)
	}
}

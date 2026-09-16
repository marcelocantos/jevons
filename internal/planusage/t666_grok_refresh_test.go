// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func t666Grok401(at time.Time) claudia.PlanUsage {
	return claudia.PlanUsage{
		Provider: claudia.ProviderGrok, Status: claudia.PlanUsageUnavailable,
		Reason: "grok billing: HTTP 401 (token may be expired — run `grok login`)", FetchedAt: at,
	}
}

func t666GrokOK(at time.Time) claudia.PlanUsage {
	reset := at.Add(3 * 24 * time.Hour)
	return claudia.PlanUsage{
		Provider: claudia.ProviderGrok, Status: claudia.PlanUsageAvailable, FetchedAt: at,
		Windows: []claudia.PlanWindow{{Name: claudia.PlanWindowWeekly, UsedPercent: pct(71), RemainingPercent: pct(29), ResetsAt: &reset}},
	}
}

// A fetch whose Grok reading is a 401 until the token has been rotated.
type t666World struct {
	rotated atomic.Bool
	fetches atomic.Int32
	now     time.Time
}

func (w *t666World) fetch(context.Context) ([]claudia.PlanUsage, error) {
	w.fetches.Add(1)
	if w.rotated.Load() {
		return []claudia.PlanUsage{claudeReading(w.now), t666GrokOK(w.now)}, nil
	}
	return []claudia.PlanUsage{claudeReading(w.now), t666Grok401(w.now)}, nil
}

func t666Reader(t *testing.T, w *t666World, refresher planusage.GrokTokenRefresher, events *[]map[string]any) *planusage.Reader {
	t.Helper()
	return planusage.NewReader(planusage.ReaderArgs{
		Fetch:            w.fetch,
		Now:              func() time.Time { return w.now },
		GrokTokenRefresh: refresher,
		LogEvent: func(component, decision string, fields map[string]any) {
			if component == "plan_usage" && decision == "grok_token_refresh" {
				*events = append(*events, fields)
			}
		},
	})
}

func t666GrokBackend(t *testing.T, r *planusage.Reader) planusage.Backend {
	t.Helper()
	for _, b := range r.Snapshot().Backends {
		if b.Provider == "grok" {
			return b
		}
	}
	t.Fatal("no grok backend in snapshot")
	return planusage.Backend{}
}

// 🎯T666 acceptance 1 and 2: a 401 runs the one-shot once, the re-poll
// recovers, the outcome is journalled.
func TestT666Grok401RunsOneShotThenRecovers(t *testing.T) {
	w := &t666World{now: fixedNow}
	var calls atomic.Int32
	var events []map[string]any
	r := t666Reader(t, w, func(context.Context) error {
		calls.Add(1)
		w.rotated.Store(true)
		return nil
	}, &events)

	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("one-shot ran %d times, want 1", got)
	}
	if got := w.fetches.Load(); got != 2 {
		t.Fatalf("fetches = %d, want the poll plus one re-poll", got)
	}
	if b := t666GrokBackend(t, r); b.Status != planusage.StatusAvailable {
		t.Fatalf("grok still %s after the rotation: %+v", b.Status, b)
	}
	if len(events) != 1 || events[0]["outcome"] != "recovered" {
		t.Fatalf("journal = %+v", events)
	}
	if !planusage.GrokBilling401([]claudia.PlanUsage{t666Grok401(fixedNow)}) || planusage.GrokBilling401([]claudia.PlanUsage{t666GrokOK(fixedNow)}) {
		t.Fatal("GrokBilling401 misclassifies")
	}
}

// The one-shot is rate-limited: a second 401 inside the window is left
// alone; after the window it runs again.
func TestT666OneShotIsRateLimited(t *testing.T) {
	w := &t666World{now: fixedNow}
	var calls atomic.Int32
	var events []map[string]any
	r := t666Reader(t, w, func(context.Context) error {
		calls.Add(1)
		return errors.New("simulated: grok one-shot could not reach auth.x.ai")
	}, &events)

	_ = r.Refresh(context.Background())
	w.now = w.now.Add(time.Minute)
	_ = r.Refresh(context.Background())
	if got := calls.Load(); got != 1 {
		t.Fatalf("one-shot ran %d times inside the window, want 1", got)
	}
	if b := t666GrokBackend(t, r); b.Status != planusage.StatusUnavailable {
		t.Fatalf("a failed one-shot must leave the 401 in place, got %s", b.Status)
	}
	if len(events) != 1 || events[0]["outcome"] != "one_shot_failed" {
		t.Fatalf("journal = %+v", events)
	}
	w.now = w.now.Add(planusage.DefaultGrokRefreshWindow)
	_ = r.Refresh(context.Background())
	if got := calls.Load(); got != 2 {
		t.Fatalf("one-shot ran %d times after the window, want 2", got)
	}
}

// A Grok unavailable for any other reason never spends a one-shot.
func TestT666NonAuthReasonsDoNotRunTheOneShot(t *testing.T) {
	var calls atomic.Int32
	var events []map[string]any
	r := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return []claudia.PlanUsage{{
				Provider: claudia.ProviderGrok, Status: claudia.PlanUsageUnavailable,
				Reason: "read grok auth (run `grok login`): open ~/.grok/auth.json: no such file", FetchedAt: fixedNow,
			}}, nil
		},
		Now:              func() time.Time { return fixedNow },
		GrokTokenRefresh: func(context.Context) error { calls.Add(1); return nil },
		LogEvent: func(_ string, _ string, fields map[string]any) {
			events = append(events, fields)
		},
	})
	_ = r.Refresh(context.Background())
	if calls.Load() != 0 || len(events) != 0 {
		t.Fatalf("not-signed-in ran the one-shot: calls=%d events=%+v", calls.Load(), events)
	}
}

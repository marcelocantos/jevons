// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T689: a reload storm must not be a vendor request storm — the shape
// that rate-limited Anthropic's usage endpoint on 2026-09-20 and, through
// 🎯T677, parked a live worker. That floor lives in claudia (🎯T85,
// PlanThrottleForcedInterval), host-wide and per provider, where every door
// to a vendor passes. A second floor in this reader only stopped a hard
// reload from asking claudia at all: on 2026-09-30 the owner consumed a
// Claude reset, watched the vendor dashboard read 0%, reloaded, and the
// cockpit kept the old number. So every RefreshNow reaches the producer.

func t689Reader(t *testing.T, now *time.Time, soft, forced *atomic.Int64) *Reader {
	t.Helper()
	reading := func() []claudia.PlanUsage {
		return []claudia.PlanUsage{{
			Provider: claudia.ProviderClaude,
			Status:   claudia.PlanUsageAvailable,
			Windows: []claudia.PlanWindow{{
				Name:             claudia.PlanWindowWeekly,
				RemainingPercent: floatPtr(42),
			}},
		}}
	}
	return NewReader(ReaderArgs{
		Now: func() time.Time { return *now },
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			soft.Add(1)
			return reading(), nil
		},
		ForceFetch: func(context.Context) ([]claudia.PlanUsage, error) {
			forced.Add(1)
			return reading(), nil
		},
	})
}

func TestT689EveryReloadReachesClaudia(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 40, 0, 0, time.UTC)
	var soft, forced atomic.Int64
	r := t689Reader(t, &now, &soft, &forced)

	// The background poll lands, then the owner reloads twice inside a
	// minute of it — the 2026-09-30 shape. Each reload is a forced
	// question to claudia, which decides whether the vendor is asked.
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		now = now.Add(20 * time.Second)
		if err := r.RefreshNow(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := forced.Load(); got != 2 {
		t.Fatalf("two reloads reached claudia %d times, want 2 — a reload answered from this reader's cache never asks anyone", got)
	}
	if got := soft.Load(); got != 1 {
		t.Fatalf("reloads used the unforced producer: soft=%d, want 1", got)
	}
	if snap := r.Snapshot(); len(snap.Backends) != 1 || len(snap.Backends[0].Windows) != 1 {
		t.Fatalf("reading lost across reloads: %+v", snap.Backends)
	}
}

func TestT689ReloadDoesNotJoinTheBackgroundPoll(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var forced atomic.Int64
	r := NewReader(ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			entered <- struct{}{}
			<-release
			return nil, nil
		},
		ForceFetch: func(context.Context) ([]claudia.PlanUsage, error) {
			forced.Add(1)
			return nil, nil
		},
		FetchTimeout: 2 * time.Second,
	})
	go r.Refresh(context.Background())
	<-entered
	done := make(chan error, 1)
	go func() { done <- r.RefreshNow(context.Background()) }()
	// Hold the background round open until the reload is parked on it.
	for parked := false; !parked; {
		r.mu.Lock()
		parked = r.refreshing != nil && forced.Load() == 0
		r.mu.Unlock()
		runtime.Gosched()
		time.Sleep(time.Millisecond)
		if parked {
			time.Sleep(20 * time.Millisecond)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := forced.Load(); got != 1 {
		t.Fatalf("a reload during the background poll took its cached answer: forced=%d, want 1", got)
	}
}

func TestT689TheBackgroundPollIsUnaffected(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var soft, forced atomic.Int64
	r := t689Reader(t, &now, &soft, &forced)
	for i := 0; i < 3; i++ {
		if err := r.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	if soft.Load() != 3 || forced.Load() != 0 {
		t.Fatalf("background polls: soft=%d forced=%d, want 3/0", soft.Load(), forced.Load())
	}
}

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

func TestT653RefreshNowUsesForceFetch(t *testing.T) {
	var soft, forced atomic.Int32
	r := NewReader(ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			soft.Add(1)
			return []claudia.PlanUsage{{
				Provider: claudia.ProviderGrok, Status: claudia.PlanUsageUnavailable, Reason: "cache",
			}}, nil
		},
		ForceFetch: func(context.Context) ([]claudia.PlanUsage, error) {
			forced.Add(1)
			return []claudia.PlanUsage{{
				Provider: claudia.ProviderGrok, Status: claudia.PlanUsageAvailable,
				Windows: []claudia.PlanWindow{{Name: claudia.PlanWindowWeekly, RemainingPercent: floatPtr(30)}},
			}}, nil
		},
		Now: func() time.Time { return time.Date(2026, 9, 15, 7, 5, 0, 0, time.UTC) },
	})
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if soft.Load() != 1 || forced.Load() != 0 {
		t.Fatalf("periodic Refresh used force: soft=%d forced=%d", soft.Load(), forced.Load())
	}
	if err := r.RefreshNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if forced.Load() != 1 {
		t.Fatalf("RefreshNow must ForceFetch, forced=%d", forced.Load())
	}
	be, ok := r.Snapshot().Backend("grok")
	if !ok || !be.Available() {
		t.Fatalf("after RefreshNow: %+v ok=%v", be, ok)
	}
}

func TestT653RefreshNowCoalesces(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var n atomic.Int32
	r := NewReader(ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			t.Fatal("periodic Fetch must not run")
			return nil, nil
		},
		ForceFetch: func(context.Context) ([]claudia.PlanUsage, error) {
			n.Add(1)
			entered <- struct{}{}
			<-release
			return []claudia.PlanUsage{{Provider: claudia.ProviderGrok, Status: claudia.PlanUsageUnavailable}}, nil
		},
		FetchTimeout: 2 * time.Second,
	})
	done := make(chan error, 2)
	go func() { done <- r.RefreshNow(context.Background()) }()
	<-entered
	go func() { done <- r.RefreshNow(context.Background()) }()
	time.Sleep(30 * time.Millisecond)
	if n.Load() != 1 {
		t.Fatalf("second RefreshNow started another fetch: %d", n.Load())
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n.Load() != 1 {
		t.Fatalf("coalesce: fetches=%d", n.Load())
	}
}

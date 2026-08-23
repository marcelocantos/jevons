// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestWaitReadyUnblocksOnFirstSuccessfulRefresh(t *testing.T) {
	release := make(chan struct{})
	r := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(ctx context.Context) ([]claudia.PlanUsage, error) {
			select {
			case <-release:
				return []claudia.PlanUsage{{
					Provider:  claudia.ProviderClaude,
					Status:    claudia.PlanUsageAvailable,
					FetchedAt: time.Now(),
					Windows: []claudia.PlanWindow{{
						Name:             claudia.PlanWindowWeekly,
						RemainingPercent: pct(40),
					}},
				}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})
	if !r.Snapshot().Pending {
		t.Fatal("expected pending before first fetch")
	}

	errCh := make(chan error, 1)
	go func() { errCh <- r.WaitReady(context.Background()) }()

	select {
	case err := <-errCh:
		t.Fatalf("WaitReady returned before refresh: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("WaitReady: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitReady did not unblock after successful refresh")
	}
	if r.Snapshot().Pending {
		t.Fatal("snapshot still pending after refresh")
	}
}

func TestWaitReadyStaysBlockedOnFetchFailure(t *testing.T) {
	r := planusage.NewReader(planusage.ReaderArgs{
		Fetch: func(context.Context) ([]claudia.PlanUsage, error) {
			return nil, errors.New("provider down")
		},
	})
	if err := r.Refresh(context.Background()); err == nil {
		t.Fatal("expected refresh error")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	err := r.WaitReady(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitReady = %v, want deadline (failure must not unblock)", err)
	}
	if !r.Snapshot().Pending {
		t.Fatal("failed fetch must leave snapshot pending")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestHandlePlanUsageLongPollsUntilFirstBatch(t *testing.T) {
	s := &Server{}
	var mu sync.Mutex
	pending := true
	ready := make(chan struct{})

	s.SetPlanUsageSource(func() any {
		mu.Lock()
		defer mu.Unlock()
		if pending {
			return planusage.Snapshot{Pending: true}
		}
		return planusage.Snapshot{
			Backends: []planusage.Backend{{
				Provider: "claude",
				Status:   planusage.StatusAvailable,
			}},
		}
	})
	s.SetPlanUsageWaitReady(func(ctx context.Context) error {
		select {
		case <-ready:
			mu.Lock()
			pending = false
			mu.Unlock()
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/plan-usage", nil)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handlePlanUsage(rr, req)
	}()

	select {
	case <-done:
		t.Fatal("handler returned before first batch landed")
	case <-time.After(30 * time.Millisecond):
	}

	close(ready)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after first batch")
	}

	var snap planusage.Snapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	if snap.Pending {
		t.Fatalf("still pending after long-poll: %s", rr.Body.String())
	}
	if len(snap.Backends) != 1 || snap.Backends[0].Provider != "claude" {
		t.Fatalf("backends = %+v", snap.Backends)
	}
}

func TestHandlePlanUsageReturnsPendingOnLongPollTimeout(t *testing.T) {
	s := &Server{}
	s.SetPlanUsageSource(func() any { return planusage.Snapshot{Pending: true} })
	s.SetPlanUsageWaitReady(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/plan-usage", nil)
	rr := httptest.NewRecorder()
	start := time.Now()
	s.handlePlanUsage(rr, req)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("handler took %v; should honour request deadline", elapsed)
	}

	var snap planusage.Snapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	if !snap.Pending {
		t.Fatalf("want pending after timeout, got %s", rr.Body.String())
	}
}

func TestHandlePlanUsageSkipsWaitWhenAlreadyReady(t *testing.T) {
	s := &Server{}
	s.SetPlanUsageSource(func() any {
		return planusage.Snapshot{
			Backends: []planusage.Backend{{Provider: "codex", Status: planusage.StatusAvailable}},
		}
	})
	waited := false
	s.SetPlanUsageWaitReady(func(context.Context) error {
		waited = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/plan-usage", nil)
	rr := httptest.NewRecorder()
	s.handlePlanUsage(rr, req)
	if waited {
		t.Fatal("WaitReady must not run when snapshot is not pending")
	}
	var snap planusage.Snapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Pending || len(snap.Backends) != 1 {
		t.Fatalf("unexpected payload: %s", rr.Body.String())
	}
}

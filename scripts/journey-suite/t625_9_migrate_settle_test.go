// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func migrateServer(t *testing.T, refusals int32, refusal string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := calls.Add(1); n <= refusals {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"` + refusal + `"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// 🎯T625.9: an in-flight refusal is re-asked until it settles.
func TestT625_9MigrateWaitsForInFlightTurnToSettle(t *testing.T) {
	srv, calls := migrateServer(t, 1, "Migrate: turn in flight; wait")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postOverseerMigrateWhenSettled(ctx, srv.URL, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2", calls.Load())
	}
}

// Controls: any other refusal fails at once; a turn that never settles is
// still reported when the deadline passes.
func TestT625_9MigrateRefusalsStillFail(t *testing.T) {
	srv, calls := migrateServer(t, 100, "no transcript found")
	if err := postOverseerMigrateWhenSettled(context.Background(), srv.URL, []byte(`{}`)); err == nil || calls.Load() != 1 {
		t.Fatalf("other 409 must fail at once: err=%v calls=%d", err, calls.Load())
	}
	srv2, _ := migrateServer(t, 100, "turn in flight")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := postOverseerMigrateWhenSettled(ctx, srv2.URL, []byte(`{}`)); err == nil {
		t.Fatal("a turn that never settles must fail at the deadline")
	}
}

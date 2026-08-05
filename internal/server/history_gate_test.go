// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/chatlog"
)

// 🎯T259: concurrent /api/history is bounded so large chatlogs cannot burn
// multiple cores under progressive hydrate.
func TestHistoryHydrateConcurrentBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	clog, err := chatlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })
	for i := 0; i < 40; i++ {
		if err := clog.Append(`{"type":"user","message":{"content":"q"}}`); err != nil {
			t.Fatal(err)
		}
	}

	s := New("test", dir)
	s.SetChatLog(clog)
	// Hold each request under the gate long enough for siblings to pile up.
	s.historySlowHook = func() { time.Sleep(40 * time.Millisecond) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/history", s.handleHistory)

	const n = 8
	var wg sync.WaitGroup
	var okCount atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/history?end=40&limit=10", nil)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				okCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if okCount.Load() != n {
		t.Fatalf("ok responses = %d, want %d", okCount.Load(), n)
	}
	peak := s.historyGate.max.Load()
	if peak > maxHistoryHydrateConcurrent {
		t.Fatalf("peak concurrent history handlers = %d, want ≤ %d", peak, maxHistoryHydrateConcurrent)
	}
	if peak < 1 {
		t.Fatal("expected at least one in-flight history handler")
	}
	// Capacity is 1: with 8 concurrent callers + sleep, peak must be exactly 1.
	if maxHistoryHydrateConcurrent == 1 && peak != 1 {
		t.Fatalf("with capacity 1, peak must be 1, got %d", peak)
	}

	// Body shape still valid (cheap ReadRange + coalesce).
	req := httptest.NewRequest(http.MethodGet, "/api/history?end=40&limit=5", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Start int               `json:"start"`
		Total int               `json:"total"`
		Lines []json.RawMessage `json:"lines"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 40 || len(body.Lines) == 0 {
		t.Fatalf("history body total=%d lines=%d", body.Total, len(body.Lines))
	}
}

func TestHistoryGateAcquireRelease(t *testing.T) {
	g := newHistoryHydrateGate(2)
	g.acquire()
	g.acquire()
	if g.max.Load() != 2 {
		t.Fatalf("max = %d, want 2", g.max.Load())
	}
	g.release()
	g.release()
	if g.in.Load() != 0 {
		t.Fatalf("in-flight after release = %d", g.in.Load())
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestT653RefreshQueryPollsProducer(t *testing.T) {
	s := &Server{}
	var n atomic.Int32
	s.SetPlanUsageSource(func() any {
		return planusage.Snapshot{
			Backends: []planusage.Backend{{Provider: "grok", Status: planusage.StatusAvailable}},
		}
	})
	s.SetPlanUsageRefresh(func(context.Context) error {
		n.Add(1)
		return nil
	})

	plain := httptest.NewRequest(http.MethodGet, "/api/plan-usage", nil)
	s.handlePlanUsage(httptest.NewRecorder(), plain)
	if n.Load() != 0 {
		t.Fatal("plain GET must not poll")
	}

	kick := httptest.NewRequest(http.MethodGet, "/api/plan-usage?refresh=1", nil)
	s.handlePlanUsage(httptest.NewRecorder(), kick)
	if n.Load() != 1 {
		t.Fatalf("refresh=1 polls: got %d", n.Load())
	}
}

func TestT653RefreshFalseIsNotAPoll(t *testing.T) {
	s := &Server{}
	var n atomic.Int32
	s.SetPlanUsageSource(func() any { return planusage.Snapshot{} })
	s.SetPlanUsageRefresh(func(context.Context) error {
		n.Add(1)
		return nil
	})
	req := httptest.NewRequest(http.MethodGet, "/api/plan-usage?refresh=0", nil)
	s.handlePlanUsage(httptest.NewRecorder(), req)
	if n.Load() != 0 {
		t.Fatal("refresh=0 must not poll")
	}
}

func TestT653MuxOpenKicksRefresh(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetPlanUsageSource(func() any { return planusage.Snapshot{} })
	done := make(chan struct{})
	s.SetPlanUsageRefresh(func(context.Context) error {
		close(done)
		return nil
	})
	buf := &replayBuf{}
	sess := &muxSession{send: make(chan []byte, 4), transcripts: map[string]*muxWatch{}}
	s.handleMuxEnvelope(t.Context(), buf, sess, muxEnvelope{V: 1, Ch: planUsageChannel, T: "open"})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("mux open must kick a producer poll")
	}
}

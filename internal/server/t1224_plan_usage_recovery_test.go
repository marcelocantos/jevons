// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"

	"github.com/coder/websocket"
)

// The old refresh flag is now a read: collection belongs to Claudia's schedule.
func TestT1224SubscribedJSONUnchangedByGET(t *testing.T) {
	s := &Server{}
	raw := json.RawMessage(`{ "backends": [], "error":"<meter> & unavailable" }`)
	s.SetPlanUsageSource(func() any { return raw })
	for _, url := range []string{"/api/plan-usage", "/api/plan-usage?refresh=1"} {
		rr := httptest.NewRecorder()
		s.handlePlanUsage(rr, httptest.NewRequest(http.MethodGet, url, nil))
		if rr.Body.String() != string(raw) {
			t.Fatalf("payload changed: %q", rr.Body.String())
		}
	}
	encoded, err := encodeMux(planUsageChannel, "frame", raw)
	if err != nil {
		t.Fatal(err)
	}
	var env muxEnvelope
	if err := json.Unmarshal(encoded, &env); err != nil {
		t.Fatal(err)
	}
	if string(env.Body) != string(raw) {
		t.Fatalf("WS changed SQL bytes: %s", encoded)
	}
}

func TestT1224MuxFanCoalescesWhenSendQueueFull(t *testing.T) {
	s := New("test", t.TempDir())
	s.mux = newMuxHub()
	rem := 11.0
	s.SetPlanUsageSource(func() any {
		return planusage.Snapshot{
			Backends: []planusage.Backend{{
				Provider: "grok",
				Status:   planusage.StatusAvailable,
				Windows:  []planusage.Window{{Name: "weekly", RemainingPercent: &rem}},
			}},
		}
	})
	sess := &muxSession{send: make(chan []byte, 1), transcripts: map[string]*muxWatch{}}
	sess.setPlanUsage(true)
	s.mux.add(sess)
	sess.send <- []byte("blocked-transcript-frame")

	s.FanPlanUsage()
	var first []byte
	select {
	case raw := <-sess.send:
		first = raw
	default:
		t.Fatal("blocked frame must remain in send queue")
	}
	if string(first) != "blocked-transcript-frame" {
		t.Fatalf("must not discard unrelated mux payload, got %q", first)
	}
	sess.mu.Lock()
	pending := sess.planUsagePending
	sess.mu.Unlock()
	if len(pending) == 0 {
		t.Fatal("plan usage must be held in pending slot when queue is full")
	}
	sess.tryFlushPlanUsagePending()
	var got muxEnvelope
	select {
	case raw := <-sess.send:
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("flush after write must enqueue latest plan-usage frame")
	}
	if got.Ch != planUsageChannel || got.T != "frame" {
		t.Fatalf("env=%+v", got)
	}
}

func TestT1224MuxOpenReadsSubscribedPayload(t *testing.T) {
	s := New("test", t.TempDir())
	raw := json.RawMessage(`{"backends":[{"provider":"claude","status":"available","windows":[{"name":"weekly","remaining_percent":55}]}]}`)
	s.SetPlanUsageSource(func() any { return raw })
	buf := &replayBuf{}
	sess := &muxSession{send: make(chan []byte, 4), transcripts: map[string]*muxWatch{}}
	s.handleMuxEnvelope(t.Context(), buf, sess, muxEnvelope{V: 1, Ch: planUsageChannel, T: "open"})
	if len(buf.frames) != 1 {
		t.Fatalf("frames=%d", len(buf.frames))
	}
	body, _ := json.Marshal(buf.frames[0]["body"])
	if !strings.Contains(string(body), `"remaining_percent":55`) {
		t.Fatalf("body=%s", body)
	}
}

func TestT1224PlanUsageMuxOpenViaRealSocket(t *testing.T) {
	s := New("test", t.TempDir())
	rem := 77.0
	s.SetPlanUsageSource(func() any {
		return planusage.Snapshot{
			Backends: []planusage.Backend{{
				Provider: "claude",
				Status:   planusage.StatusAvailable,
				Windows:  []planusage.Window{{Name: "weekly", RemainingPercent: &rem}},
			}},
		}
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/mux", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	_, hello, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	if !strings.Contains(string(hello), `"t":"hello"`) {
		t.Fatalf("expected hello, got %s", hello)
	}
	open, _ := json.Marshal(muxEnvelope{V: 1, Ch: planUsageChannel, T: "open"})
	if err := conn.Write(ctx, websocket.MessageText, open); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.Contains(string(data), planUsageChannel) && strings.Contains(string(data), `"remaining_percent":77`) {
			return
		}
	}
	t.Fatal("mux open must deliver cached plan-usage frame from server callers")
}

// A real mux socket writer drains the same session that concurrent fans fill.
// Non-usage frames are never eligible for replacement; final usage wins.
func TestT1224RealMuxWriterConcurrentFanAndFrames(t *testing.T) {
	s := New("test", t.TempDir())
	var value atomic.Int32
	s.SetPlanUsageSource(func() any {
		rem := float64(value.Load())
		return planusage.Snapshot{Backends: []planusage.Backend{{Provider: "claude", Status: planusage.StatusAvailable, Windows: []planusage.Window{{Name: "weekly", RemainingPercent: &rem}}}}}
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/mux", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	open, _ := json.Marshal(muxEnvelope{V: 1, Ch: planUsageChannel, T: "open"})
	if err := conn.Write(ctx, websocket.MessageText, open); err != nil {
		t.Fatal(err)
	}
	var sess *muxSession
	deadline := time.Now().Add(time.Second)
	for sess == nil && time.Now().Before(deadline) {
		s.mux.mu.Lock()
		for candidate := range s.mux.conns {
			if candidate.watchingPlanUsage() {
				sess = candidate
			}
		}
		s.mux.mu.Unlock()
		if sess == nil {
			time.Sleep(time.Millisecond)
		}
	}
	if sess == nil {
		t.Fatal("no subscribed production socket")
	}
	const count = 100
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for i := 0; i < count; i++ {
			raw, _ := encodeMux("transcript:fixture", "frame", map[string]int{"seq": i})
			for !sess.enqueue(raw) {
				time.Sleep(time.Microsecond)
			}
			value.Store(int32(i + 1))
			s.FanPlanUsage()
		}
		value.Store(999)
		s.FanPlanUsage()
	}()
	got := 0
	latest := float64(0)
	for got < count || latest != 999 {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read got=%d latest=%v: %v", got, latest, err)
		}
		var env struct {
			Ch   string `json:"ch"`
			Body struct {
				Seq      int `json:"seq"`
				Backends []struct {
					Windows []struct {
						RemainingPercent *float64 `json:"remaining_percent"`
					} `json:"windows"`
				} `json:"backends"`
			} `json:"body"`
		}
		if json.Unmarshal(raw, &env) != nil {
			continue
		}
		if env.Ch == "transcript:fixture" {
			if env.Body.Seq != got {
				t.Fatalf("frame order: got %d expected %d", env.Body.Seq, got)
			}
			got++
		}
		if env.Ch == planUsageChannel && len(env.Body.Backends) > 0 && len(env.Body.Backends[0].Windows) > 0 && env.Body.Backends[0].Windows[0].RemainingPercent != nil {
			latest = *env.Body.Backends[0].Windows[0].RemainingPercent
		}
	}
	<-sent
}

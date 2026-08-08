// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// pipeConn is an in-memory FrameConn: frames written by the "provider"
// side arrive at the hub's ReadFrame and vice versa.
type pipeConn struct {
	in     chan []byte // hub reads from here (provider → hub)
	out    chan []byte // hub writes here (hub → provider)
	closed chan struct{}
	once   sync.Once
}

func newPipeConn() *pipeConn {
	return &pipeConn{
		in:     make(chan []byte, 64),
		out:    make(chan []byte, 64),
		closed: make(chan struct{}),
	}
}

func (p *pipeConn) ReadFrame(ctx context.Context) ([]byte, error) {
	select {
	case data := <-p.in:
		return data, nil
	case <-p.closed:
		return nil, errors.New("closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *pipeConn) WriteFrame(ctx context.Context, data []byte) error {
	select {
	case p.out <- data:
		return nil
	case <-p.closed:
		return errors.New("closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *pipeConn) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

// providerSend queues a provider→hub frame.
func (p *pipeConn) providerSend(t *testing.T, f feedFrame) {
	t.Helper()
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	select {
	case p.in <- data:
	case <-time.After(2 * time.Second):
		t.Fatal("providerSend: hub not reading")
	}
}

// providerExpect reads the next hub→provider frame and returns it.
func (p *pipeConn) providerExpect(t *testing.T, op string) feedFrame {
	t.Helper()
	select {
	case data := <-p.out:
		var f feedFrame
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("unmarshal hub frame: %v", err)
		}
		if f.Op != op {
			t.Fatalf("expected op %q, got %q", op, f.Op)
		}
		return f
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for hub op %q", op)
		return feedFrame{}
	}
}

func feedManifest(id string, feeds ...string) Manifest {
	m := Manifest{ID: id, Version: "1.0.0", Contract: ContractMajor}
	for _, f := range feeds {
		m.Capabilities.Feeds = append(m.Capabilities.Feeds, FeedCap{
			Name: f, Schema: id + "." + f + ".v1", Replay: true,
		})
	}
	return m
}

func ev(origin, feed string, seq int64, kind string) FeedEvent {
	return FeedEvent{
		Feed: feed, Seq: seq, TS: time.Now().UTC(),
		Origin: origin, Kind: kind,
	}
}

// attachAndHandshake runs Attach in a goroutine and completes the
// describe→describe_ok→subscribe exchange for one feed manifest.
func attachAndHandshake(t *testing.T, h *FeedHub, m Manifest) (*pipeConn, chan error, []feedFrame) {
	t.Helper()
	conn := newPipeConn()
	done := make(chan error, 1)
	go func() { done <- h.Attach(context.Background(), conn) }()
	conn.providerExpect(t, "describe")
	conn.providerSend(t, feedFrame{Op: "describe_ok", Manifest: &m})
	var subs []feedFrame
	for range m.Capabilities.Feeds {
		subs = append(subs, conn.providerExpect(t, "subscribe"))
	}
	return conn, done, subs
}

func TestFeedHubHandshakeSubscribeAndFold(t *testing.T) {
	reg := NewRegistry()
	var mu sync.Mutex
	var got []FeedEvent
	h := NewFeedHub(FeedHubArgs{
		Registry: reg,
		OnEvent: func(id string, e FeedEvent) {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		},
	})

	conn, done, subs := attachAndHandshake(t, h, feedManifest("alpha", "health"))
	if subs[0].Feed != "health" || subs[0].From != 0 {
		t.Fatalf("expected subscribe health from 0, got %+v", subs[0])
	}

	conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("alpha", "health", 1, "up"))})
	conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("alpha", "health", 2, "up"))})
	waitFor(t, "2 dispatched events", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 2
	})

	if n := len(reg.ModelFeed("alpha", "health")); n != 2 {
		t.Fatalf("model folded %d events, want 2", n)
	}
	sts := h.Statuses()
	if len(sts) != 1 || sts[0].State != FeedOK || sts[0].Events != 2 {
		t.Fatalf("unexpected status %+v", sts)
	}

	snap := h.Snapshot()
	if snap["alpha"]["health"].Count != 2 || snap["alpha"]["health"].Last.Seq != 2 {
		t.Fatalf("unexpected snapshot %+v", snap)
	}

	conn.Close()
	if err := <-done; err == nil {
		t.Fatal("Attach should return an error on connection close")
	}
	waitFor(t, "disconnected status", func() bool {
		sts := h.Statuses()
		return len(sts) == 1 && sts[0].State == FeedDisconnected
	})
}

func ptr[T any](v T) *T { return &v }

func TestFeedHubCursorResumeAcrossRestart(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := NewRegistry()
	h := NewFeedHub(FeedHubArgs{Registry: reg, Store: store})

	conn, done, subs := attachAndHandshake(t, h, feedManifest("alpha", "health"))
	if subs[0].From != 0 {
		t.Fatalf("first subscribe should be full replay, got from=%d", subs[0].From)
	}
	conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("alpha", "health", 7, "up"))})
	waitFor(t, "cursor persisted", func() bool {
		seq, _ := store.GetCursor("alpha", "health")
		return seq == 7
	})
	conn.Close()
	<-done

	// "Restart": fresh hub over the same store resumes from last+1.
	h2 := NewFeedHub(FeedHubArgs{Registry: NewRegistry(), Store: store})
	conn2, done2, subs2 := attachAndHandshake(t, h2, feedManifest("alpha", "health"))
	if subs2[0].From != 8 {
		t.Fatalf("resume subscribe should be from=8, got %d", subs2[0].From)
	}
	// Manifest persisted from the first attach.
	if m, ok, err := store.GetManifest("alpha"); err != nil || !ok || m.ID != "alpha" {
		t.Fatalf("manifest not persisted: %v %v %+v", ok, err, m)
	}
	conn2.Close()
	<-done2
}

func TestFeedHubLoopSafetyAndDedup(t *testing.T) {
	reg := NewRegistry()
	reg.HubID = "jevons"
	var mu sync.Mutex
	var got []FeedEvent
	h := NewFeedHub(FeedHubArgs{
		Registry: reg,
		OnEvent: func(id string, e FeedEvent) {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		},
	})

	conn, done, _ := attachAndHandshake(t, h, feedManifest("alpha", "health"))
	conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("alpha", "health", 1, "up"))})
	// Self-origin relay: dropped (§5.4).
	conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("jevons", "health", 2, "echo"))})
	// Duplicate (origin, feed, seq): dropped.
	conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("alpha", "health", 1, "up"))})
	conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("alpha", "health", 3, "up"))})

	waitFor(t, "2 folded events", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 2
	})
	mu.Lock()
	seqs := []int64{got[0].Seq, got[1].Seq}
	mu.Unlock()
	if seqs[0] != 1 || seqs[1] != 3 {
		t.Fatalf("unexpected folded seqs %v", seqs)
	}
	conn.Close()
	<-done
}

// TestFeedHubSlowConsumerDegradesWithoutWedging is the acceptance
// oracle for graceful degradation: a sink that never drains must not
// block the fold loop; the provider's status turns degraded with a
// dropped count, and the events still fold into the model.
func TestFeedHubSlowConsumerDegradesWithoutWedging(t *testing.T) {
	reg := NewRegistry()
	block := make(chan struct{})
	h := NewFeedHub(FeedHubArgs{
		Registry:  reg,
		QueueSize: 2,
		OnEvent: func(id string, e FeedEvent) {
			<-block // wedged sink: never returns until unblocked
		},
	})

	conn, done, _ := attachAndHandshake(t, h, feedManifest("alpha", "health"))
	// 1 in the wedged sink, 2 in the queue, the rest must drop — and
	// every send must complete promptly (the fold loop never blocks).
	const total = 10
	for i := 1; i <= total; i++ {
		conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("alpha", "health", int64(i), "tick"))})
	}
	waitFor(t, "all events folded", func() bool {
		return len(reg.ModelFeed("alpha", "health")) == total
	})
	waitFor(t, "degraded status with drops", func() bool {
		sts := h.Statuses()
		return len(sts) == 1 && sts[0].State == FeedDegraded &&
			sts[0].Dropped > 0 && sts[0].Events == total
	})
	// Unblock the sink before expecting Attach to return — the
	// dispatcher WaitGroup drains remaining queue entries on detach.
	close(block)
	conn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Attach did not return after sink unblocked")
	}
}

// TestFeedHubStalledHandshakeIsolated: a provider that connects and
// never answers describe is dropped at the handshake timeout, while a
// healthy provider attached concurrently keeps folding — one stalled
// provider never wedges the hub or its siblings.
func TestFeedHubStalledHandshakeIsolated(t *testing.T) {
	reg := NewRegistry()
	var mu sync.Mutex
	var got []FeedEvent
	h := NewFeedHub(FeedHubArgs{
		Registry:         reg,
		HandshakeTimeout: 50 * time.Millisecond,
		OnEvent: func(id string, e FeedEvent) {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		},
	})

	// Stalled: never answers describe.
	stalled := newPipeConn()
	stalledDone := make(chan error, 1)
	go func() { stalledDone <- h.Attach(context.Background(), stalled) }()
	stalled.providerExpect(t, "describe")

	// Healthy sibling attaches and folds while the other stalls.
	conn, done, _ := attachAndHandshake(t, h, feedManifest("beta", "pulse"))
	conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("beta", "pulse", 1, "tick"))})
	waitFor(t, "healthy provider folds", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})

	select {
	case err := <-stalledDone:
		if err == nil {
			t.Fatal("stalled handshake should error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stalled provider not dropped at handshake timeout")
	}
	conn.Close()
	<-done
}

func TestFeedHubDisallowedProviderRefused(t *testing.T) {
	h := NewFeedHub(FeedHubArgs{
		Registry: NewRegistry(),
		Allowed:  func(id string) bool { return id == "alpha" },
	})
	conn := newPipeConn()
	done := make(chan error, 1)
	go func() { done <- h.Attach(context.Background(), conn) }()
	conn.providerExpect(t, "describe")
	m := feedManifest("mallory", "health")
	conn.providerSend(t, feedFrame{Op: "describe_ok", Manifest: &m})
	if err := <-done; err == nil {
		t.Fatal("disallowed provider should be refused")
	}
}

func TestFeedHubReconnectReplacesStaleConn(t *testing.T) {
	h := NewFeedHub(FeedHubArgs{Registry: NewRegistry()})
	m := feedManifest("alpha", "health")

	conn1, done1, _ := attachAndHandshake(t, h, m)
	// Reconnect with the same id: the stale socket is closed and its
	// death report must not stomp the new connection's ok status.
	conn2, done2, _ := attachAndHandshake(t, h, m)
	if err := <-done1; err == nil {
		t.Fatal("stale connection should have been closed by adopt")
	}
	waitFor(t, "status stays ok after stale conn death", func() bool {
		sts := h.Statuses()
		return len(sts) == 1 && sts[0].State == FeedOK
	})

	conn2.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("alpha", "health", 1, "up"))})
	waitFor(t, "new connection folds", func() bool {
		return h.registry.Cursor("alpha", "health") == 1
	})
	conn2.Close()
	<-done2
	_ = conn1
}

func TestFeedHubSetDeclsSeedsDisconnected(t *testing.T) {
	h := NewFeedHub(FeedHubArgs{Registry: NewRegistry()})
	h.SetDecls(declsOf("alpha", "beta"))
	sts := h.Statuses()
	if len(sts) != 2 {
		t.Fatalf("want 2 seeded statuses, got %+v", sts)
	}
	for _, st := range sts {
		if st.State != FeedDisconnected {
			t.Fatalf("seeded status should be disconnected: %+v", st)
		}
	}
	// Removal prunes, keeping only still-declared ids.
	h.SetDecls(declsOf("beta"))
	sts = h.Statuses()
	if len(sts) != 1 || sts[0].ID != "beta" {
		t.Fatalf("prune failed: %+v", sts)
	}
}

func TestFeedHubModelCap(t *testing.T) {
	reg := NewRegistry()
	h := NewFeedHub(FeedHubArgs{Registry: reg})
	conn, done, _ := attachAndHandshake(t, h, feedManifest("alpha", "health"))
	for i := 1; i <= modelCap+10; i++ {
		conn.providerSend(t, feedFrame{Op: "event", Event: ptr(ev("alpha", "health", int64(i), "tick"))})
	}
	waitFor(t, "cursor reaches final seq", func() bool {
		return reg.Cursor("alpha", "health") == int64(modelCap+10)
	})
	evs := reg.ModelFeed("alpha", "health")
	if len(evs) != modelCap {
		t.Fatalf("model retained %d events, want cap %d", len(evs), modelCap)
	}
	if evs[len(evs)-1].Seq != int64(modelCap+10) {
		t.Fatalf("cap should keep the newest events, last seq %d", evs[len(evs)-1].Seq)
	}
	conn.Close()
	<-done
}

func declsOf(ids ...string) []config.ProviderDecl {
	out := make([]config.ProviderDecl, 0, len(ids))
	for _, id := range ids {
		out = append(out, config.ProviderDecl{ID: id, Transport: config.ProviderTransportConnect})
	}
	return out
}

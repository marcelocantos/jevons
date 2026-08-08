// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

// 🎯T27.5 acceptance oracle, server half: a provider that dials
// /ws/provider and pushes feed events reaches connected /ws/remote
// clients as provider_event broadcasts; a client connecting later gets
// the aggregated model in its init provider_model frame; and
// GET /api/providers surfaces feed status. All hermetic — a fake
// provider over httptest, no daemon, no Grok.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/marcelocantos/jevons/internal/provider"
)

func dialWS(t *testing.T, ctx context.Context, base, path string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+path, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func readFrameOfType(t *testing.T, ctx context.Context, conn *websocket.Conn, typ string) map[string]any {
	t.Helper()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read waiting for %q: %v", typ, err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m["type"] == typ {
			return m
		}
	}
}

func TestProviderFeedToClientBroadcast(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)

	hub := provider.NewFeedHub(provider.FeedHubArgs{
		Registry: provider.NewRegistry(),
		OnEvent: func(id string, ev provider.FeedEvent) {
			s.Broadcast(map[string]any{"type": "provider_event", "provider": id, "event": ev})
		},
		OnStatus: func(st provider.FeedStatus) {
			s.Broadcast(map[string]any{"type": "provider_status", "status": st})
		},
	})
	s.SetProviderFeeds(hub)

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Client first, so it observes the live broadcast.
	client := dialWS(t, ctx, srv.URL, "/ws/remote")
	readFrameOfType(t, ctx, client, "init")

	// Fake provider attaches: describe → describe_ok → subscribe.
	prov := dialWS(t, ctx, srv.URL, "/ws/provider")
	readProviderFrame(t, ctx, prov, "describe")

	manifest := map[string]any{
		"id": "alpha", "version": "1.0.0", "contract": "1",
		"capabilities": map[string]any{
			"feeds": []map[string]any{{"name": "health", "schema": "alpha.health.v1", "replay": true}},
		},
	}
	writeWS(t, ctx, prov, map[string]any{"op": "describe_ok", "manifest": manifest})
	sub := readProviderFrame(t, ctx, prov, "subscribe")
	if sub["feed"] != "health" {
		t.Fatalf("expected subscribe for health, got %v", sub)
	}

	// Push one event; the connected client must receive the broadcast.
	writeWS(t, ctx, prov, map[string]any{"op": "event", "event": map[string]any{
		"feed": "health", "seq": 1, "ts": time.Now().UTC().Format(time.RFC3339),
		"origin": "alpha", "kind": "up",
	}})
	got := readFrameOfType(t, ctx, client, "provider_event")
	evt, _ := got["event"].(map[string]any)
	if got["provider"] != "alpha" || evt["feed"] != "health" || evt["seq"] != float64(1) {
		t.Fatalf("unexpected provider_event %v", got)
	}

	// A client connecting after the fact gets the folded model on init.
	late := dialWS(t, ctx, srv.URL, "/ws/remote")
	readFrameOfType(t, ctx, late, "init")
	model := readFrameOfType(t, ctx, late, "provider_model")
	raw, _ := json.Marshal(model["model"])
	var snap map[string]map[string]provider.FeedSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("provider_model shape: %v", err)
	}
	if snap["alpha"]["health"].Count != 1 || snap["alpha"]["health"].Last.Seq != 1 {
		t.Fatalf("unexpected snapshot %v", snap)
	}

	// Observability: /api/providers reports the feed status as ok.
	resp, err := http.Get(srv.URL + "/api/providers")
	if err != nil {
		t.Fatalf("GET /api/providers: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/providers status %d", resp.StatusCode)
	}
	var obs struct {
		Feeds []provider.FeedStatus `json:"feeds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&obs); err != nil {
		t.Fatalf("decode /api/providers: %v", err)
	}
	if len(obs.Feeds) != 1 || obs.Feeds[0].ID != "alpha" || obs.Feeds[0].State != provider.FeedOK {
		t.Fatalf("unexpected /api/providers feeds %+v", obs.Feeds)
	}
}

// writeWS marshals and sends one JSON text frame.
func writeWS(t *testing.T, ctx context.Context, conn *websocket.Conn, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readProviderFrame reads hub→provider frames until op matches.
func readProviderFrame(t *testing.T, ctx context.Context, conn *websocket.Conn, op string) map[string]any {
	t.Helper()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("provider read waiting for op %q: %v", op, err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m["op"] == op {
			return m
		}
	}
}

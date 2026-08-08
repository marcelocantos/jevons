// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

// 🎯T27.8 end-to-end hermetic oracle (server half): a mnemo-shaped peer
// attaches over /ws/provider, its health feed reaches /ws/remote clients
// and the aggregated model, its ViewNode surface is composed for the
// client renderer, and a declared MCP tool is re-exported under the
// mnemo__ namespace via the generic aggregator path. No production
// mnemo special-case — the peer id is just config + wire frames.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/provider"
	"github.com/marcelocantos/jevons/internal/ui"
)

func TestMnemoProviderE2E_FeedUIAndMCP(t *testing.T) {
	s := New("test", t.TempDir())

	reg := provider.NewRegistry()
	hub := provider.NewFeedHub(provider.FeedHubArgs{
		Registry: reg,
		Allowed:  func(id string) bool { return id == "mnemo" },
		OnEvent: func(id string, ev provider.FeedEvent) {
			s.Broadcast(map[string]any{
				"type": "provider_event", "provider": id, "event": ev,
			})
		},
		OnUI: s.BroadcastProviderView,
	})
	// Desired set includes mnemo (connect-mode declaration).
	hub.SetDecls([]config.ProviderDecl{{
		ID: "mnemo", Transport: config.ProviderTransportConnect,
		Params: map[string]any{
			"url":     "http://127.0.0.1:19419/health",
			"mcp_url": "http://127.0.0.1:19419/mcp",
		},
	}})
	s.SetProviderFeeds(hub)

	// Fake MCP server tools that the aggregator would re-export.
	sink := &e2eSink{tools: map[string]bool{}}
	agg := provider.NewAggregator(provider.AggregatorArgs{
		Sink: sink,
		Dial: func(ctx context.Context, endpoint string) (provider.MCPToolClient, error) {
			return &e2eMCP{
				tools: []mcp.Tool{
					mcp.NewTool("mnemo_status", mcp.WithDescription("mnemo health")),
					mcp.NewTool("mnemo_search", mcp.WithDescription("search")),
				},
			}, nil
		},
	})
	agg.SetDecls([]config.ProviderDecl{{
		ID: "mnemo", Transport: config.ProviderTransportConnect,
		Params: map[string]any{"mcp_url": "http://127.0.0.1:19419/mcp", "url": "http://127.0.0.1:19419/health"},
	}})
	// Simulate lifecycle Running so aggregator attaches.
	agg.HandlePhase(provider.Health{ID: "mnemo", Phase: provider.PhaseRunning})
	t.Cleanup(agg.Shutdown)

	// Wait for MCP tools to land.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sink.has("mnemo__mnemo_status") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sink.has("mnemo__mnemo_status") {
		t.Fatal("expected mnemo__mnemo_status re-exported via aggregator")
	}
	if !sink.has("mnemo__mnemo_search") {
		t.Fatal("expected mnemo__mnemo_search re-exported via aggregator")
	}

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := dialWS(t, ctx, srv.URL, "/ws/remote")
	readFrameOfType(t, ctx, client, "init")

	// mnemo-shaped peer attaches (mirrors mnemo internal/jevonsprovider).
	prov := dialWS(t, ctx, srv.URL, "/ws/provider")
	readProviderFrame(t, ctx, prov, "describe")
	writeWS(t, ctx, prov, map[string]any{
		"op": "describe_ok",
		"manifest": map[string]any{
			"id": "mnemo", "version": "0.85.0-test", "contract": "1",
			"capabilities": map[string]any{
				"feeds": []map[string]any{{
					"name": "health", "schema": "mnemo.health.v1", "replay": true,
				}},
				"ui": []map[string]any{{
					"surface": "mnemo.status", "title": "mnemo",
					"feeds": []string{"health"},
					"root": map[string]any{
						"type": "vstack", "id": "mnemo.root",
						"props": map[string]any{"spacing": 8},
						"children": []map[string]any{
							{"type": "text", "id": "mnemo.title",
								"props": map[string]any{"text": "mnemo", "font": "headline"}},
							{"type": "text", "id": "mnemo.health",
								"props": map[string]any{"text": "ok 15 fail 0"}},
							{"type": "button", "id": "mnemo.refresh",
								"props": map[string]any{"text": "Refresh", "action": "refresh"}},
						},
					},
				}},
				"mcp": map[string]any{
					"transport": "http",
					"endpoint":  "http://127.0.0.1:19419/mcp",
				},
			},
			"egress": false,
		},
	})
	sub := readProviderFrame(t, ctx, prov, "subscribe")
	if sub["feed"] != "health" {
		t.Fatalf("subscribe feed=%v", sub["feed"])
	}

	// UI composed → client view frame.
	view := readFrameOfType(t, ctx, client, "view")
	if view["slot"] != ui.SlotProviders {
		t.Fatalf("slot=%v", view["slot"])
	}
	root := decodeStrictView(t, view)
	if root.ID != ui.RootID || len(root.Children) < 1 {
		t.Fatalf("composed root=%+v", root)
	}
	// Find mnemo section by id prefix.
	found := false
	for _, ch := range root.Children {
		if strings.Contains(ch.ID, "mnemo") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no mnemo section in composed UI: %+v", root.Children)
	}

	// Feed event → aggregated model + client broadcast.
	writeWS(t, ctx, prov, map[string]any{
		"op": "event",
		"event": map[string]any{
			"feed": "health", "seq": 1,
			"ts":     time.Now().UTC().Format(time.RFC3339),
			"origin": "mnemo", "kind": "tick",
			"data": map[string]any{"ok": 15, "fail": 0},
		},
	})
	evFrame := readFrameOfType(t, ctx, client, "provider_event")
	if evFrame["provider"] != "mnemo" {
		t.Fatalf("provider_event=%v", evFrame)
	}

	// Aggregated model holds the feed.
	snap := hub.Snapshot()
	feeds, ok := snap["mnemo"]
	if !ok || feeds["health"].Count < 1 {
		t.Fatalf("snapshot=%+v", snap)
	}
	if last := feeds["health"].Last; last == nil || last.Origin != "mnemo" {
		t.Fatalf("last event=%+v", feeds["health"].Last)
	}

	// GET /api/providers surfaces feed status.
	resp, err := http.Get(srv.URL + "/api/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
}

// e2eSink records re-exported tool names.
type e2eSink struct {
	mu    sync.Mutex
	tools map[string]bool
}

func (s *e2eSink) AddProviderTool(t mcp.Tool, _ func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[t.Name] = true
}

func (s *e2eSink) RemoveProviderTools(names ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range names {
		delete(s.tools, n)
	}
}

func (s *e2eSink) has(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tools[name]
}

type e2eMCP struct {
	tools []mcp.Tool
}

func (c *e2eMCP) ListTools(ctx context.Context) ([]mcp.Tool, error) { return c.tools, nil }
func (c *e2eMCP) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("ok"), nil
}
func (c *e2eMCP) Close() error { return nil }

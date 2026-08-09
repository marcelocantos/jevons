// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

// 🎯T27.6 acceptance oracle, smoke half (à la T9): a provider surface
// round-trips to the T9/T11 client lane without an app rebuild. A fake
// provider attaches over /ws/provider declaring a ViewNode surface
// bound to a feed; a /ws/remote client receives the composed view;
// pushing a feed event re-renders (a fresh view frame arrives); a
// late-joining client is seeded the composed view on init; and a
// namespaced client action relays back to the provider's feed socket.
// The client decode is strict — unknown fields fail — mirroring the
// Swift ViewNode Codable shape, so this stands in for the renderer
// hermetically (on-device visual remains the class-3 human residual).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/provider"
	"github.com/marcelocantos/jevons/internal/ui"
)

// clientViewNode mirrors Swift ViewNode's Codable shape. Strict decode:
// a composed tree carrying fields the shipped client cannot parse fails
// here, not on device.
type clientViewNode struct {
	Type     string           `json:"type"`
	ID       string           `json:"id"`
	Props    map[string]any   `json:"props"`
	Children []clientViewNode `json:"children"`
}

func decodeStrictView(t *testing.T, frame map[string]any) clientViewNode {
	t.Helper()
	raw, err := json.Marshal(frame["root"])
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var root clientViewNode
	if err := dec.Decode(&root); err != nil {
		t.Fatalf("view root does not decode as client ViewNode: %v", err)
	}
	return root
}

func TestProviderUISmokeRoundTrip(t *testing.T) {
	s := New("test", t.TempDir())

	hub := provider.NewFeedHub(provider.FeedHubArgs{
		Registry: provider.NewRegistry(),
		OnUI:     s.BroadcastProviderView,
	})
	s.SetProviderFeeds(hub)

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Client first, so it observes the composed-view broadcast live.
	client := dialWS(t, ctx, srv.URL, "/ws/remote")
	readFrameOfType(t, ctx, client, "init")

	// Provider attaches declaring one feed and one surface bound to it.
	prov := dialWS(t, ctx, srv.URL, "/ws/provider")
	readProviderFrame(t, ctx, prov, "describe")
	writeWS(t, ctx, prov, map[string]any{"op": "describe_ok", "manifest": map[string]any{
		"id": "wire", "version": "1.0.0", "contract": "1",
		"capabilities": map[string]any{
			"feeds": []map[string]any{{"name": "health", "schema": "wire.health.v1", "replay": true}},
			"ui": []map[string]any{{
				"surface": "wire.panel",
				"title":   "Wire",
				"feeds":   []string{"health"},
				"root": map[string]any{
					"type": "vstack", "id": "root",
					"children": []map[string]any{
						{"type": "text", "id": "label", "props": map[string]any{"text": "hello"}},
						{"type": "button", "id": "go", "props": map[string]any{"text": "Go", "action": "refresh"}},
					},
				},
			}},
		},
	}})
	readProviderFrame(t, ctx, prov, "subscribe")

	// 1. Attach composes: client receives the provider surface at the
	// expected slot, strictly decodable by the T9/T11 renderer shape.
	view := readFrameOfType(t, ctx, client, "view")
	if view["slot"] != ui.SlotProviders {
		t.Fatalf("slot = %v", view["slot"])
	}
	root := decodeStrictView(t, view)
	if root.ID != ui.RootID || len(root.Children) != 1 {
		t.Fatalf("unexpected composed root %+v", root)
	}
	section := root.Children[0]
	if section.ID != "provider/wire/wire.panel" {
		t.Fatalf("section id = %q", section.ID)
	}
	if got := section.Children[0].Props["text"]; got != "Wire" {
		t.Fatalf("section title = %v", got)
	}
	panel := section.Children[1]
	if panel.ID != "wire/root" || panel.Children[0].Props["text"] != "hello" {
		t.Fatalf("panel = %+v", panel)
	}
	if got := panel.Children[1].Props["action"]; got != "provider/wire/wire.panel/refresh" {
		t.Fatalf("namespaced action = %v", got)
	}

	// 2. Push an update on the bound feed: the client re-renders (a
	// fresh view frame arrives) — no app rebuild, no reconnect.
	writeWS(t, ctx, prov, map[string]any{"op": "event", "event": map[string]any{
		"feed": "health", "seq": 1, "ts": time.Now().UTC().Format(time.RFC3339),
		"origin": "wire", "kind": "up",
	}})
	rerender := readFrameOfType(t, ctx, client, "view")
	if rerender["slot"] != ui.SlotProviders {
		t.Fatalf("re-render slot = %v", rerender["slot"])
	}
	decodeStrictView(t, rerender)

	// 3. A late-joining client is seeded the composed view on init.
	late := dialWS(t, ctx, srv.URL, "/ws/remote")
	readFrameOfType(t, ctx, late, "init")
	seeded := readFrameOfType(t, ctx, late, "view")
	if seeded["slot"] != ui.SlotProviders {
		t.Fatalf("seeded slot = %v", seeded["slot"])
	}

	// 4. Action round-trip: the namespaced client action reaches the
	// owning provider's feed socket as a §4.1 action frame.
	writeWS(t, ctx, client, map[string]any{
		"type": "action", "action": "provider/wire/wire.panel/refresh", "value": "now",
	})
	act := readProviderFrame(t, ctx, prov, "action")
	if act["surface"] != "wire.panel" || act["action"] != "refresh" || act["value"] != "now" {
		t.Fatalf("relayed action = %v", act)
	}
}

func TestProviderUINoSurfacesNoView(t *testing.T) {
	// A hub with no attached surfaces must not emit empty view frames.
	s := New("test", t.TempDir())
	s.SetProviderFeeds(provider.NewFeedHub(provider.FeedHubArgs{Registry: provider.NewRegistry()}))
	if msg, ok := s.providerViewMessage(); ok {
		t.Fatalf("unexpected view message %+v", msg)
	}
}

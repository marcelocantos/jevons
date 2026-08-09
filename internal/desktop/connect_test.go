// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package desktop_test

// 🎯T27.7 connect oracle: the head launches (as a Client), connects to
// jevonsd (/ws/remote), and section membership tracks the aggregated
// model under provider attach / toggle.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/desktop"
	"github.com/marcelocantos/jevons/internal/provider"
	"github.com/marcelocantos/jevons/internal/server"
)

func TestHeadConnectsAndTracksSections(t *testing.T) {
	s := server.New("test", t.TempDir())
	r := provider.NewRegistry()
	hub := provider.NewFeedHub(provider.FeedHubArgs{
		Registry: r,
		OnUI:     s.BroadcastProviderView,
	})
	s.SetProviderFeeds(hub)

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Snapshot API before any provider: zero sections.
	resp, err := http.Get(srv.URL + "/api/desktop/head")
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		SectionCount int               `json:"section_count"`
		Sections     []desktop.Section `json:"sections"`
		Supersedes   []string          `json:"supersedes"`
		Inventory    []desktop.Signal  `json:"inventory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if snap.SectionCount != 0 {
		t.Fatalf("empty hub section_count=%d", snap.SectionCount)
	}
	if len(snap.Supersedes) != 3 {
		t.Fatalf("supersedes = %v", snap.Supersedes)
	}
	if miss := desktop.InventoryComplete(snap.Inventory); len(miss) > 0 {
		t.Fatalf("API inventory incomplete: %v", miss)
	}

	// Two enabled providers in the aggregated model.
	r.SetSurfaces("alpha", []provider.UISurface{{
		Surface: "alpha.panel", Title: "Alpha",
		Root: &provider.ViewNode{Type: "text", ID: "t", Props: map[string]any{"text": "A"}},
	}})
	r.SetSurfaces("beta", []provider.UISurface{{
		Surface: "beta.panel", Title: "Beta",
		Root: &provider.ViewNode{Type: "text", ID: "t", Props: map[string]any{"text": "B"}},
	}})
	s.BroadcastProviderView()

	resp2, err := http.Get(srv.URL + "/api/desktop/head")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp2.Body).Decode(&snap); err != nil {
		resp2.Body.Close()
		t.Fatal(err)
	}
	resp2.Body.Close()
	if snap.SectionCount != 2 {
		t.Fatalf("want 2 sections, got %d (%+v)", snap.SectionCount, snap.Sections)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Client connects and receives the seeded view.
	head := desktop.NewHead()
	client := &desktop.Client{BaseURL: srv.URL, Head: head, DialTimeout: 5 * time.Second}
	model, err := client.FetchSectionsOnce(ctx)
	if err != nil {
		t.Fatalf("FetchSectionsOnce: %v", err)
	}
	if !model.Connected {
		t.Fatal("head not connected after FetchSectionsOnce")
	}
	if len(model.Sections) != 2 {
		t.Fatalf("client sections = %+v", model.Sections)
	}

	// Toggle beta off.
	r.ClearSurfaces("beta")
	s.BroadcastProviderView()

	resp3, err := http.Get(srv.URL + "/api/desktop/head")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp3.Body).Decode(&snap); err != nil {
		resp3.Body.Close()
		t.Fatal(err)
	}
	resp3.Body.Close()
	if snap.SectionCount != 1 || snap.Sections[0].ProviderID != "alpha" {
		t.Fatalf("after toggle: %+v", snap)
	}

	// Fresh client sees the toggled model.
	head2 := desktop.NewHead()
	client2 := &desktop.Client{BaseURL: srv.URL, Head: head2, DialTimeout: 5 * time.Second}
	model2, err := client2.FetchSectionsOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(model2.Sections) != 1 || model2.Sections[0].ProviderID != "alpha" {
		t.Fatalf("client after toggle: %+v", model2.Sections)
	}
}

func TestSetDeclsClearsDisabledProviderSurfaces(t *testing.T) {
	// Toggle via FeedHub.SetDecls: disable removes section from model.
	r := provider.NewRegistry()
	r.SetSurfaces("keep", []provider.UISurface{{
		Surface: "keep.p", Title: "Keep",
		Root: &provider.ViewNode{Type: "text", ID: "t", Props: map[string]any{"text": "k"}},
	}})
	r.SetSurfaces("drop", []provider.UISurface{{
		Surface: "drop.p", Title: "Drop",
		Root: &provider.ViewNode{Type: "text", ID: "t", Props: map[string]any{"text": "d"}},
	}})
	hub := provider.NewFeedHub(provider.FeedHubArgs{Registry: r})
	if n := len(desktop.SectionsFromSurfaces(hub.ComposedUI())); n != 2 {
		t.Fatalf("pre-toggle sections = %d", n)
	}

	enable := true
	disable := false
	hub.SetDecls([]config.ProviderDecl{
		{ID: "keep", Transport: config.ProviderTransportConnect, Enable: &enable, Params: map[string]any{"url": "http://127.0.0.1:9"}},
		{ID: "drop", Transport: config.ProviderTransportConnect, Enable: &disable, Params: map[string]any{"url": "http://127.0.0.1:9"}},
	})
	secs := desktop.SectionsFromSurfaces(hub.ComposedUI())
	if len(secs) != 1 || secs[0].ProviderID != "keep" {
		t.Fatalf("after disable drop: %+v", secs)
	}

	// Re-enable drop: surfaces stay cleared until re-attach; section stays gone
	// until SetSurfaces runs again (attach path). That is the product: disable
	// tears the section down; enable alone does not invent a surface.
	hub.SetDecls([]config.ProviderDecl{
		{ID: "keep", Transport: config.ProviderTransportConnect, Enable: &enable, Params: map[string]any{"url": "http://127.0.0.1:9"}},
		{ID: "drop", Transport: config.ProviderTransportConnect, Enable: &enable, Params: map[string]any{"url": "http://127.0.0.1:9"}},
	})
	if n := len(desktop.SectionsFromSurfaces(hub.ComposedUI())); n != 1 {
		t.Fatalf("re-enable without re-attach should not invent surfaces, got %d", n)
	}
	r.SetSurfaces("drop", []provider.UISurface{{
		Surface: "drop.p", Title: "Drop",
		Root: &provider.ViewNode{Type: "text", ID: "t", Props: map[string]any{"text": "d"}},
	}})
	if n := len(desktop.SectionsFromSurfaces(hub.ComposedUI())); n != 2 {
		t.Fatalf("after re-attach drop: %d", n)
	}
}

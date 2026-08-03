// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/provider"
)

// 🎯T27.1 oracle: mock registers; all three capabilities observable via hub.

func TestConformanceDescribeThreeCapabilities(t *testing.T) {
	reg := provider.NewRegistry()
	mock := provider.NewMock()
	if err := reg.Register(mock); err != nil {
		t.Fatalf("register: %v", err)
	}
	m, ok := reg.Manifest(provider.MockID)
	if !ok {
		t.Fatal("manifest missing after register")
	}
	if err := provider.ValidateManifest(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if m.Contract != provider.ContractMajor {
		t.Fatalf("contract=%q", m.Contract)
	}
	if m.Egress {
		t.Fatal("mock must declare egress:false (opt-in)")
	}
	if len(m.Capabilities.Feeds) != 1 || m.Capabilities.Feeds[0].Name != provider.MockFeedName {
		t.Fatalf("feeds=%+v", m.Capabilities.Feeds)
	}
	if !m.Capabilities.Feeds[0].Replay {
		t.Fatal("mock feed must advertise replay")
	}
	if len(m.Capabilities.UI) != 1 || m.Capabilities.UI[0].Surface != provider.MockSurfaceName {
		t.Fatalf("ui=%+v", m.Capabilities.UI)
	}
	if m.Capabilities.MCP == nil || m.Capabilities.MCP.Transport != provider.MCPTransportHTTP {
		t.Fatalf("mcp=%+v", m.Capabilities.MCP)
	}
	if m.Capabilities.MCP.Endpoint == "" {
		t.Fatal("mcp endpoint required")
	}
}

func TestConformanceFeedCursorAndNoReingest(t *testing.T) {
	reg := provider.NewRegistry()
	mock := provider.NewMock()
	if err := reg.Register(mock); err != nil {
		t.Fatal(err)
	}
	mock.AppendPulse("tick", map[string]any{"n": 1})
	mock.AppendPulse("tick", map[string]any{"n": 2})
	mock.AppendPulse("tick", map[string]any{"n": 3})

	first, err := reg.Subscribe(provider.MockID, provider.MockFeedName, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 {
		t.Fatalf("first fold len=%d want 3", len(first))
	}
	for i, ev := range first {
		if ev.Seq != int64(i+1) {
			t.Fatalf("seq order: got %d at %d", ev.Seq, i)
		}
		if ev.Origin != provider.MockID {
			t.Fatalf("origin=%q", ev.Origin)
		}
	}
	if reg.Cursor(provider.MockID, provider.MockFeedName) != 3 {
		t.Fatalf("cursor=%d", reg.Cursor(provider.MockID, provider.MockFeedName))
	}

	// Simulated restart: resume from last+1 — History returns empty; re-subscribe
	// from 1 must not double-fold already-seen events if we re-pull history.
	again, err := reg.Subscribe(provider.MockID, provider.MockFeedName, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("re-ingest folded %d events; want 0 (loop-safety/seen)", len(again))
	}
	model := reg.ModelFeed(provider.MockID, provider.MockFeedName)
	if len(model) != 3 {
		t.Fatalf("model len=%d want 3 (no duplicates)", len(model))
	}

	// Live push of already-seen seq is dropped.
	if reg.Ingest(provider.MockID, first[0]) {
		t.Fatal("duplicate seq must be dropped")
	}
}

func TestConformanceLoopSafetyHubOrigin(t *testing.T) {
	reg := provider.NewRegistry()
	reg.HubID = "jevons"
	mock := provider.NewMock()
	if err := reg.Register(mock); err != nil {
		t.Fatal(err)
	}
	// Provider relays a hub-origin event (must not re-enter model).
	relayed := provider.FeedEvent{
		Feed:   provider.MockFeedName,
		Seq:    99,
		Origin: "jevons",
		Kind:   "decision",
		Data:   map[string]any{"note": "should drop"},
	}
	if reg.Ingest(provider.MockID, relayed) {
		t.Fatal("hub-origin event must be dropped")
	}
	if len(reg.ModelFeed(provider.MockID, provider.MockFeedName)) != 0 {
		t.Fatal("model must stay empty")
	}
}

func TestConformanceUISurfaceAndActionRoundTrip(t *testing.T) {
	reg := provider.NewRegistry()
	mock := provider.NewMock()
	if err := reg.Register(mock); err != nil {
		t.Fatal(err)
	}
	composed := reg.ComposedUI()
	surfaces, ok := composed[provider.MockID]
	if !ok || len(surfaces) != 1 {
		t.Fatalf("composed=%+v", composed)
	}
	root, ok := reg.SurfaceRoot(provider.MockID, provider.MockSurfaceName)
	if !ok || root == nil {
		t.Fatal("missing surface root")
	}
	golden := provider.MockGoldenRoot()
	gotJSON, _ := json.Marshal(root)
	wantJSON, _ := json.Marshal(golden)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("golden mismatch\ngot  %s\nwant %s", gotJSON, wantJSON)
	}
	if err := reg.RelayAction(provider.MockID, provider.MockSurfaceName, "refresh", ""); err != nil {
		t.Fatal(err)
	}
	acts := mock.Actions()
	if len(acts) != 1 || acts[0].Name != "refresh" || acts[0].Surface != provider.MockSurfaceName {
		t.Fatalf("actions=%+v", acts)
	}
}

func TestConformanceMCPToolObservable(t *testing.T) {
	reg := provider.NewRegistry()
	mock := provider.NewMock()
	if err := reg.Register(mock); err != nil {
		t.Fatal(err)
	}
	tools := reg.ListTools()
	if len(tools) != 1 {
		t.Fatalf("tools=%+v", tools)
	}
	if tools[0].Name != provider.MockToolName {
		t.Fatalf("name=%q", tools[0].Name)
	}
	wantQ := provider.MockID + "__" + provider.MockToolName
	if tools[0].Qualified != wantQ {
		t.Fatalf("qualified=%q want %q", tools[0].Qualified, wantQ)
	}
	res, err := reg.CallTool(wantQ, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res["pong"] != true {
		t.Fatalf("result=%v", res)
	}
}

func TestConformanceNoPerProviderCode(t *testing.T) {
	// Load-bearing: provider-handling path must not hard-code third-party ids.
	// Scan this package's .go files (excluding tests) for forbidden identifiers.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Concrete providers that must not appear as hub special-cases.
	forbidden := []string{`"mnemo"`, `"doit"`, `"bullseye"`, `"ytt"`}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		src := string(body)
		for _, f := range forbidden {
			if strings.Contains(src, f) {
				t.Errorf("%s contains hard-coded provider id %s", name, f)
			}
		}
	}
}

func TestValidateManifestRejectsBadContract(t *testing.T) {
	err := provider.ValidateManifest(provider.Manifest{
		ID: "x", Version: "1", Contract: "99",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterDuplicateFails(t *testing.T) {
	reg := provider.NewRegistry()
	if err := reg.Register(provider.NewMock()); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(provider.NewMock()); err == nil {
		t.Fatal("expected duplicate error")
	}
}

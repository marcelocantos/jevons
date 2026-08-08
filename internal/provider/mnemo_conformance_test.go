// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider_test

// 🎯T27.8 joint oracle (hermetic half): a mnemo-shaped provider passes
// the same T27.1 hub surface as the reference mock — feed fold, UI
// surface, MCP tool namespace — with zero production special-casing.
// Real mnemo's peer lives in the mnemo repo (internal/jevonsprovider);
// this fixture mirrors its contract shapes so the hub path is proven
// generic. Production package sources still forbid hard-coded "mnemo"
// (TestConformanceNoPerProviderCode).

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/provider"
)

// Fixed shapes aligned with mnemo internal/jevonsprovider constants.
const (
	mnemoID           = "mnemo"
	mnemoVersion      = "0.85.0-test"
	mnemoFeed         = "health"
	mnemoFeedSchema   = "mnemo.health.v1"
	mnemoSurface      = "mnemo.status"
	mnemoSurfaceTitle = "mnemo"
	mnemoTool         = "mnemo_status"
	mnemoMCP          = "http://127.0.0.1:19419/mcp"
)

// mnemoFixture is an in-process Provider with mnemo's contract shapes.
type mnemoFixture struct {
	mu      sync.Mutex
	seq     int64
	events  []provider.FeedEvent
	actions []provider.Action
}

func newMnemoFixture() *mnemoFixture { return &mnemoFixture{} }

func (m *mnemoFixture) Describe() provider.Manifest {
	root := mnemoStatusRoot("ok")
	return provider.Manifest{
		ID:       mnemoID,
		Version:  mnemoVersion,
		Contract: provider.ContractMajor,
		Capabilities: provider.Capabilities{
			Feeds: []provider.FeedCap{{
				Name: mnemoFeed, Schema: mnemoFeedSchema, Replay: true,
			}},
			UI: []provider.UISurface{{
				Surface: mnemoSurface, Title: mnemoSurfaceTitle,
				Feeds: []string{mnemoFeed}, Root: &root,
			}},
			MCP: &provider.MCPEndpoint{
				Transport: provider.MCPTransportHTTP,
				Endpoint:  mnemoMCP,
			},
		},
		Egress: false,
	}
}

func mnemoStatusRoot(status string) provider.ViewNode {
	return provider.ViewNode{
		Type: "vstack", ID: "mnemo.root",
		Props: map[string]any{"spacing": 8},
		Children: []provider.ViewNode{
			{Type: "text", ID: "mnemo.title", Props: map[string]any{
				"text": "mnemo", "font": "headline", "color": "primary",
			}},
			{Type: "text", ID: "mnemo.health", Props: map[string]any{
				"text": status, "font": "body",
			}},
			{Type: "button", ID: "mnemo.refresh", Props: map[string]any{
				"text": "Refresh", "action": "refresh", "style": "bordered",
			}},
		},
	}
}

func (m *mnemoFixture) AppendHealth(kind string, data map[string]any) provider.FeedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	ev := provider.FeedEvent{
		Feed: mnemoFeed, Seq: m.seq, TS: time.Now().UTC().Truncate(time.Second),
		Origin: mnemoID, Kind: kind, Data: data,
	}
	m.events = append(m.events, ev)
	return ev
}

func (m *mnemoFixture) History(feed string, from int64) ([]provider.FeedEvent, error) {
	if feed != mnemoFeed {
		return nil, fmt.Errorf("unknown feed %q", feed)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []provider.FeedEvent
	for _, ev := range m.events {
		if ev.Seq >= from {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (m *mnemoFixture) Tools() []provider.Tool {
	return []provider.Tool{{
		Provider: mnemoID, Name: mnemoTool,
		Qualified: mnemoID + "__" + mnemoTool,
	}}
}

func (m *mnemoFixture) CallTool(name string, _ map[string]any) (map[string]any, error) {
	if name != mnemoTool {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return map[string]any{"ok": true, "provider": mnemoID}, nil
}

func (m *mnemoFixture) HandleAction(surface, name, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, provider.Action{Surface: surface, Name: name, Value: value})
}

func TestMnemoConformanceDescribeThreeCapabilities(t *testing.T) {
	reg := provider.NewRegistry()
	fix := newMnemoFixture()
	if err := reg.Register(fix); err != nil {
		t.Fatalf("register: %v", err)
	}
	m, ok := reg.Manifest(mnemoID)
	if !ok {
		t.Fatal("manifest missing")
	}
	if err := provider.ValidateManifest(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if m.Contract != provider.ContractMajor {
		t.Fatalf("contract=%q", m.Contract)
	}
	if m.Egress {
		t.Fatal("mnemo must declare egress:false")
	}
	if len(m.Capabilities.Feeds) != 1 || m.Capabilities.Feeds[0].Name != mnemoFeed {
		t.Fatalf("feeds=%+v", m.Capabilities.Feeds)
	}
	if len(m.Capabilities.UI) != 1 || m.Capabilities.UI[0].Surface != mnemoSurface {
		t.Fatalf("ui=%+v", m.Capabilities.UI)
	}
	if m.Capabilities.MCP == nil || m.Capabilities.MCP.Endpoint == "" {
		t.Fatal("mcp endpoint required")
	}
}

func TestMnemoConformanceFeedUpdatesAggregatedModel(t *testing.T) {
	reg := provider.NewRegistry()
	fix := newMnemoFixture()
	if err := reg.Register(fix); err != nil {
		t.Fatal(err)
	}
	fix.AppendHealth("tick", map[string]any{"ok": 15, "fail": 0})
	fix.AppendHealth("tick", map[string]any{"ok": 15, "fail": 0})

	folded, err := reg.Subscribe(mnemoID, mnemoFeed, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(folded) != 2 {
		t.Fatalf("folded=%d want 2", len(folded))
	}
	for _, ev := range folded {
		if ev.Origin != mnemoID {
			t.Fatalf("origin=%q", ev.Origin)
		}
	}
	model := reg.ModelFeed(mnemoID, mnemoFeed)
	if len(model) != 2 {
		t.Fatalf("model len=%d", len(model))
	}
	if reg.Cursor(mnemoID, mnemoFeed) != 2 {
		t.Fatalf("cursor=%d", reg.Cursor(mnemoID, mnemoFeed))
	}
	// Resume from last+1 must not re-ingest.
	again, err := reg.Subscribe(mnemoID, mnemoFeed, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("re-ingest folded %d", len(again))
	}
}

func TestMnemoConformanceUISurfaceAndAction(t *testing.T) {
	reg := provider.NewRegistry()
	fix := newMnemoFixture()
	if err := reg.Register(fix); err != nil {
		t.Fatal(err)
	}
	composed := reg.ComposedUI()
	surfaces, ok := composed[mnemoID]
	if !ok || len(surfaces) != 1 {
		t.Fatalf("composed=%+v", composed)
	}
	root, ok := reg.SurfaceRoot(mnemoID, mnemoSurface)
	if !ok || root == nil {
		t.Fatal("missing surface root")
	}
	// Client-shaped strict decode: only type/id/props/children.
	raw, _ := json.Marshal(root)
	decType := struct {
		Type     string            `json:"type"`
		ID       string            `json:"id"`
		Props    map[string]any    `json:"props"`
		Children []json.RawMessage `json:"children"`
	}{}
	if err := json.Unmarshal(raw, &decType); err != nil {
		t.Fatal(err)
	}
	if decType.Type != "vstack" || decType.ID != "mnemo.root" {
		t.Fatalf("root=%+v", decType)
	}
	if err := reg.RelayAction(mnemoID, mnemoSurface, "refresh", ""); err != nil {
		t.Fatal(err)
	}
	fix.mu.Lock()
	n := len(fix.actions)
	fix.mu.Unlock()
	if n != 1 {
		t.Fatalf("actions=%d", n)
	}
}

func TestMnemoConformanceMCPToolCallable(t *testing.T) {
	reg := provider.NewRegistry()
	fix := newMnemoFixture()
	if err := reg.Register(fix); err != nil {
		t.Fatal(err)
	}
	tools := reg.ListTools()
	if len(tools) != 1 {
		t.Fatalf("tools=%+v", tools)
	}
	wantQ := mnemoID + "__" + mnemoTool
	if tools[0].Qualified != wantQ {
		t.Fatalf("qualified=%q", tools[0].Qualified)
	}
	res, err := reg.CallTool(wantQ, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res["ok"] != true {
		t.Fatalf("result=%v", res)
	}
}

func TestMnemoAndMockShareHubPath(t *testing.T) {
	// Same Registry code path serves both mock and mnemo fixtures —
	// the load-bearing "no per-provider code" property for T27.8.
	reg := provider.NewRegistry()
	if err := reg.Register(provider.NewMock()); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(newMnemoFixture()); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Manifest(provider.MockID); !ok {
		t.Fatal("mock missing")
	}
	if _, ok := reg.Manifest(mnemoID); !ok {
		t.Fatal("mnemo missing")
	}
	if n := len(reg.ListTools()); n != 2 {
		t.Fatalf("tools=%d want 2", n)
	}
	composed := reg.ComposedUI()
	if len(composed) != 2 {
		t.Fatalf("composed providers=%d", len(composed))
	}
}

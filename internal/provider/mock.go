// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"sync"
	"time"
)

// Fixed mock identifiers for the 🎯T27.1 conformance suite.
const (
	MockID           = "mock"
	MockVersion      = "0.1.0"
	MockFeedName     = "pulse"
	MockFeedSchema   = "mock.pulse.v1"
	MockSurfaceName  = "mock.status"
	MockSurfaceTitle = "mock"
	MockToolName     = "mock_ping"
	MockMCPEndpoint  = "http://127.0.0.1:0/mcp" // in-process only; port unused
)

// Mock is the reference provider: exactly one feed, one ViewNode surface,
// one MCP tool — all with known fixed content.
type Mock struct {
	mu     sync.Mutex
	seq    int64
	events []FeedEvent
	// actions records ActionMessage relays from the hub.
	actions []Action
}

// Action is a UI action delivered to the owning provider.
type Action struct {
	Surface string
	Name    string
	Value   string
}

// NewMock returns a ready reference provider with an empty feed history.
func NewMock() *Mock {
	return &Mock{}
}

// Describe implements the describe handshake.
func (m *Mock) Describe() Manifest {
	root := MockGoldenRoot()
	return Manifest{
		ID:       MockID,
		Version:  MockVersion,
		Contract: ContractMajor,
		Capabilities: Capabilities{
			Feeds: []FeedCap{{
				Name:   MockFeedName,
				Schema: MockFeedSchema,
				Replay: true,
			}},
			UI: []UISurface{{
				Surface: MockSurfaceName,
				Title:   MockSurfaceTitle,
				Feeds:   []string{MockFeedName},
				Root:    &root,
			}},
			MCP: &MCPEndpoint{
				Transport: MCPTransportHTTP,
				Endpoint:  MockMCPEndpoint,
			},
		},
		Egress: false,
	}
}

// MockGoldenRoot is the fixed ViewNode tree for surface mock.status.
func MockGoldenRoot() ViewNode {
	return ViewNode{
		Type: "vstack",
		ID:   "mock.root",
		Props: map[string]any{
			"spacing": 8,
		},
		Children: []ViewNode{
			{
				Type: "text",
				ID:   "mock.title",
				Props: map[string]any{
					"text":  "mock provider",
					"font":  "headline",
					"color": "primary",
				},
			},
			{
				Type: "button",
				ID:   "mock.refresh",
				Props: map[string]any{
					"text":   "Refresh",
					"action": "refresh",
					"style":  "bordered",
				},
			},
		},
	}
}

// AppendPulse appends one pulse event (for tests and live mock drivers).
func (m *Mock) AppendPulse(kind string, data map[string]any) FeedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	ev := FeedEvent{
		Feed:   MockFeedName,
		Seq:    m.seq,
		TS:     time.Now().UTC().Truncate(time.Second),
		Origin: MockID,
		Kind:   kind,
		Data:   data,
	}
	m.events = append(m.events, ev)
	return ev
}

// History returns feed events with seq >= from (inclusive), for replay.
func (m *Mock) History(feed string, from int64) ([]FeedEvent, error) {
	if feed != MockFeedName {
		return nil, fmt.Errorf("unknown feed %q", feed)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []FeedEvent
	for _, ev := range m.events {
		if ev.Seq >= from {
			out = append(out, ev)
		}
	}
	return out, nil
}

// Tools returns the single known MCP tool.
func (m *Mock) Tools() []Tool {
	return []Tool{{
		Provider:  MockID,
		Name:      MockToolName,
		Qualified: MockID + "__" + MockToolName,
	}}
}

// CallTool executes a bare tool name (pre-namespace).
func (m *Mock) CallTool(name string, _ map[string]any) (map[string]any, error) {
	if name != MockToolName {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return map[string]any{"pong": true}, nil
}

// HandleAction records a UI action (hub → provider relay).
func (m *Mock) HandleAction(surface, name, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, Action{Surface: surface, Name: name, Value: value})
}

// Actions returns a copy of recorded UI actions.
func (m *Mock) Actions() []Action {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Action, len(m.actions))
	copy(out, m.actions)
	return out
}

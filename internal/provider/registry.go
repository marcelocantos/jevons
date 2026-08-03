// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"sync"
)

// Provider is the in-process contract a registered backend must satisfy.
// Network/WS adapters implement this over the wire; the mock does it directly.
type Provider interface {
	Describe() Manifest
	History(feed string, from int64) ([]FeedEvent, error)
	Tools() []Tool
	CallTool(name string, args map[string]any) (map[string]any, error)
	HandleAction(surface, name, value string)
}

// Registry is the thin hub surface: register providers, fold feeds, compose
// UI, re-export MCP tools. jevonsd will own one of these after 🎯T27.2+.
type Registry struct {
	mu sync.Mutex

	// HubID is the origin id of the hub itself (loop-safety). Empty = no self id.
	HubID string

	providers map[string]Provider
	manifests map[string]Manifest
	// cursors[provider][feed] = last folded seq
	cursors map[string]map[string]int64
	// seen keys "origin|feed|seq"
	seen map[string]struct{}
	// model[provider][feed] = last event data by kind path — simple fold
	model map[string]map[string][]FeedEvent
	// surfaces[provider] = UI roots from last describe
	surfaces map[string][]UISurface
}

// NewRegistry returns an empty hub registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		manifests: make(map[string]Manifest),
		cursors:   make(map[string]map[string]int64),
		seen:      make(map[string]struct{}),
		model:     make(map[string]map[string][]FeedEvent),
		surfaces:  make(map[string][]UISurface),
	}
}

// Register validates describe, stores the provider, and primes surfaces.
func (r *Registry) Register(p Provider) error {
	m := p.Describe()
	if err := ValidateManifest(m); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[m.ID]; ok {
		return fmt.Errorf("provider %q already registered", m.ID)
	}
	r.providers[m.ID] = p
	r.manifests[m.ID] = m
	r.cursors[m.ID] = make(map[string]int64)
	r.model[m.ID] = make(map[string][]FeedEvent)
	// copy surfaces for composition
	ui := make([]UISurface, len(m.Capabilities.UI))
	copy(ui, m.Capabilities.UI)
	r.surfaces[m.ID] = ui
	return nil
}

// Manifest returns the stored describe result.
func (r *Registry) Manifest(id string) (Manifest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.manifests[id]
	return m, ok
}

// Subscribe replays feed history from fromSeq and folds new events into the model.
// fromSeq is inclusive (0 = full replay). Returns folded events in order.
func (r *Registry) Subscribe(providerID, feed string, fromSeq int64) ([]FeedEvent, error) {
	r.mu.Lock()
	p, ok := r.providers[providerID]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", providerID)
	}
	hist, err := p.History(feed, fromSeq)
	if err != nil {
		return nil, err
	}
	var folded []FeedEvent
	for _, ev := range hist {
		if r.fold(providerID, ev) {
			folded = append(folded, ev)
		}
	}
	return folded, nil
}

// fold applies loop-safety and cursor update. Returns true if accepted.
func (r *Registry) fold(providerID string, ev FeedEvent) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.HubID != "" && ev.Origin == r.HubID {
		return false
	}
	key := fmt.Sprintf("%s|%s|%d", ev.Origin, ev.Feed, ev.Seq)
	if _, dup := r.seen[key]; dup {
		return false
	}
	r.seen[key] = struct{}{}
	if r.cursors[providerID] == nil {
		r.cursors[providerID] = make(map[string]int64)
	}
	r.cursors[providerID][ev.Feed] = ev.Seq
	if r.model[providerID] == nil {
		r.model[providerID] = make(map[string][]FeedEvent)
	}
	r.model[providerID][ev.Feed] = append(r.model[providerID][ev.Feed], ev)
	return true
}

// Ingest folds a live push event (same loop-safety as Subscribe).
func (r *Registry) Ingest(providerID string, ev FeedEvent) bool {
	return r.fold(providerID, ev)
}

// Cursor returns the last folded seq for a feed (0 if none).
func (r *Registry) Cursor(providerID, feed string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cursors[providerID] == nil {
		return 0
	}
	return r.cursors[providerID][feed]
}

// ModelFeed returns a copy of folded events for a provider feed.
func (r *Registry) ModelFeed(providerID, feed string) []FeedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := r.model[providerID][feed]
	out := make([]FeedEvent, len(src))
	copy(out, src)
	return out
}

// ComposedUI returns a map of provider id → surfaces (with roots) for the
// multi-provider UI producer (🎯T27.6). Slot name is the surface id.
func (r *Registry) ComposedUI() map[string][]UISurface {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string][]UISurface, len(r.surfaces))
	for id, surfaces := range r.surfaces {
		cp := make([]UISurface, len(surfaces))
		copy(cp, surfaces)
		out[id] = cp
	}
	return out
}

// SurfaceRoot returns the root ViewNode for a provider surface, if any.
func (r *Registry) SurfaceRoot(providerID, surface string) (*ViewNode, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.surfaces[providerID] {
		if s.Surface == surface && s.Root != nil {
			root := *s.Root
			return &root, true
		}
	}
	return nil, false
}

// ListTools returns aggregated MCP tools from all registered providers.
func (r *Registry) ListTools() []Tool {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Tool
	for _, p := range r.providers {
		out = append(out, p.Tools()...)
	}
	return out
}

// CallTool invokes a namespaced or bare tool on the owning provider.
// Qualified form: "{provider}__{name}".
func (r *Registry) CallTool(qualified string, args map[string]any) (map[string]any, error) {
	providerID, name, err := splitQualifiedTool(qualified)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	p, ok := r.providers[providerID]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("provider %q not registered", providerID)
	}
	return p.CallTool(name, args)
}

// RelayAction delivers a UI action to the provider that owns the surface.
func (r *Registry) RelayAction(providerID, surface, action, value string) error {
	r.mu.Lock()
	p, ok := r.providers[providerID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("provider %q not registered", providerID)
	}
	p.HandleAction(surface, action, value)
	return nil
}

func splitQualifiedTool(qualified string) (providerID, name string, err error) {
	// "{id}__{tool}" — first "__" separates provider from tool (tools may contain _).
	for i := 0; i+1 < len(qualified); i++ {
		if qualified[i] == '_' && qualified[i+1] == '_' {
			providerID = qualified[:i]
			name = qualified[i+2:]
			if providerID == "" || name == "" {
				return "", "", fmt.Errorf("invalid tool name %q", qualified)
			}
			return providerID, name, nil
		}
	}
	return "", "", fmt.Errorf("tool %q not namespaced (want provider__name)", qualified)
}

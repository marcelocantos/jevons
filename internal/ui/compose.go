// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ui is the server-driven UI producer (🎯T27.6), rebuilt from the
// original internal/ui schema.go + viewstate.go around multi-provider
// composition: provider UI surfaces (declared under the 🎯T27.1 contract)
// are composed into one ViewNode tree that the T9/T11 native client
// renders generically — no per-provider client code.
//
// The node vocabulary is provider.ViewNode (which mirrors
// ios/Jevon/Models/ViewNode.swift); this package owns layout: providers
// own their panel, Jevons owns placement (contract §6.2). Data flow is
// reactive — feed event → aggregated model → surface re-render — and
// user interactions route back to the owning provider by surface
// namespace via ParseAction.
package ui

import (
	"maps"
	"sort"
	"strings"

	"github.com/marcelocantos/jevons/internal/provider"
)

// Node is the composed-tree node type, shared with the provider contract.
type Node = provider.ViewNode

// SlotProviders is the client slot the composed provider view targets.
const SlotProviders = "providers"

// RootID is the stable id of the composed tree's root node.
const RootID = "providers.root"

// actionPrefix namespaces provider-bound actions on the client wire.
const actionPrefix = "provider"

// ViewMessage sends a view tree to the client (matches Swift ViewMessage).
type ViewMessage struct {
	Type string `json:"type"` // "view"
	Root Node   `json:"root"`
	Slot string `json:"slot,omitempty"`
}

// DismissMessage tells the client to close a slot.
type DismissMessage struct {
	Type string `json:"type"` // "dismiss"
	Slot string `json:"slot"`
}

// Compose builds the aggregate provider view from per-provider surfaces
// (as returned by Registry.ComposedUI). The tree is deterministic:
// providers ordered by id, surfaces by surface id. Each surface becomes
// a titled section whose subtree is namespaced — node ids gain a
// "{provider}/" prefix and action props are rewritten to
// "provider/{provider}/{surface}/{action}" so interactions route back
// to the owning provider with no per-provider client code.
func Compose(surfaces map[string][]provider.UISurface) Node {
	root := Node{
		Type:  "vstack",
		ID:    RootID,
		Props: map[string]any{"spacing": 12, "alignment": "leading"},
	}

	pids := make([]string, 0, len(surfaces))
	for pid := range surfaces {
		pids = append(pids, pid)
	}
	sort.Strings(pids)

	for _, pid := range pids {
		ss := append([]provider.UISurface(nil), surfaces[pid]...)
		sort.Slice(ss, func(i, j int) bool { return ss[i].Surface < ss[j].Surface })
		for _, s := range ss {
			if s.Root == nil {
				continue
			}
			root.Children = append(root.Children, composeSection(pid, s))
		}
	}
	return root
}

// composeSection wraps one provider surface in its titled panel.
func composeSection(pid string, s provider.UISurface) Node {
	title := s.Title
	if title == "" {
		title = s.Surface
	}
	sectionID := actionPrefix + "/" + pid + "/" + s.Surface
	return Node{
		Type:  "vstack",
		ID:    sectionID,
		Props: map[string]any{"spacing": 6, "alignment": "leading"},
		Children: []Node{
			{
				Type: "text",
				ID:   sectionID + "/title",
				Props: map[string]any{
					"text":   title,
					"font":   "caption",
					"weight": "semibold",
					"color":  "secondary",
				},
			},
			namespaceNode(pid, s.Surface, *s.Root),
		},
	}
}

// namespaceNode deep-copies a surface subtree, prefixing node ids with
// the provider id and rewriting action props into the routed namespace.
func namespaceNode(pid, surface string, n Node) Node {
	out := Node{Type: n.Type}
	if n.ID != "" {
		out.ID = pid + "/" + n.ID
	}
	if len(n.Props) > 0 {
		props := make(map[string]any, len(n.Props))
		maps.Copy(props, n.Props)
		if a, ok := props["action"].(string); ok && a != "" {
			props["action"] = actionPrefix + "/" + pid + "/" + surface + "/" + a
		}
		out.Props = props
	}
	if len(n.Children) > 0 {
		out.Children = make([]Node, len(n.Children))
		for i, c := range n.Children {
			out.Children[i] = namespaceNode(pid, surface, c)
		}
	}
	return out
}

// ParseAction splits a routed client action back into (provider,
// surface, action). ok is false for actions outside the provider
// namespace — callers fall through to their own action handling.
func ParseAction(action string) (pid, surface, name string, ok bool) {
	parts := strings.SplitN(action, "/", 4)
	if len(parts) != 4 || parts[0] != actionPrefix ||
		parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}

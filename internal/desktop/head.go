// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package desktop is the Jevons menu-bar/tray head (🎯T27.7): a thin
// client of jevonsd that renders the aggregated multi-provider UI as one
// section per enabled provider. Section membership is driven by the
// composed ViewNode tree (🎯T27.6) — N enabled providers with surfaces
// → N sections; toggling a provider off removes its section. The
// package also holds the checkable inventory of mnemo T85/T86/T89
// signals that this head supersedes.
//
// Platform chrome (NSStatusItem / system tray) is a thin view over the
// MenuModel this package produces; decidable acceptance is asserted
// against the model, not by eye (class-3 residual for UX feel).
package desktop

import (
	"sort"
	"strings"
	"sync"

	"github.com/marcelocantos/jevons/internal/provider"
	"github.com/marcelocantos/jevons/internal/ui"
)

// Section is one tray/menu section for one enabled provider.
// Acceptance: N enabled providers → N sections (not one per surface).
type Section struct {
	ProviderID string   `json:"provider_id"`
	Title      string   `json:"title"`
	Surfaces   []string `json:"surfaces"`
}

// MenuModel is the renderable inventory the head holds. Platform UI
// (status item menu, tray popup) mirrors this structure.
type MenuModel struct {
	// Connected is true once the head has completed at least one
	// successful handoff with jevonsd (init and/or view frame).
	Connected bool      `json:"connected"`
	Sections  []Section `json:"sections"`
}

// Head is the live tray-head state machine: apply composed surfaces or
// a composed view root, observe section membership change.
type Head struct {
	mu    sync.Mutex
	model MenuModel
}

// NewHead returns an unconnected head with no sections.
func NewHead() *Head {
	return &Head{}
}

// Model returns a snapshot of the current menu model.
func (h *Head) Model() MenuModel {
	h.mu.Lock()
	defer h.mu.Unlock()
	return cloneModel(h.model)
}

// Sections returns the current section list (copy).
func (h *Head) Sections() []Section {
	return h.Model().Sections
}

// MarkConnected records that the head has established contact with jevonsd.
func (h *Head) MarkConnected() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.model.Connected = true
}

// ApplySurfaces rebuilds sections from the aggregated provider UI map
// (Registry/FeedHub.ComposedUI). One section per provider that has at
// least one surface with a Root. Deterministic order by provider id.
func (h *Head) ApplySurfaces(surfaces map[string][]provider.UISurface) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.model.Sections = SectionsFromSurfaces(surfaces)
	h.model.Connected = true
}

// ApplyComposedRoot rebuilds sections from a ui.Compose tree (slot
// "providers"). Used when the head receives a ViewMessage over /ws/remote.
func (h *Head) ApplyComposedRoot(root provider.ViewNode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.model.Sections = SectionsFromComposedRoot(root)
	h.model.Connected = true
}

// SectionsFromSurfaces derives the head's section list from the
// aggregated UI model. Providers without a renderable root are omitted
// (nothing to show). Sorted by provider id.
func SectionsFromSurfaces(surfaces map[string][]provider.UISurface) []Section {
	if len(surfaces) == 0 {
		return nil
	}
	pids := make([]string, 0, len(surfaces))
	for pid := range surfaces {
		pids = append(pids, pid)
	}
	sort.Strings(pids)

	var out []Section
	for _, pid := range pids {
		ss := surfaces[pid]
		var surfaceIDs []string
		title := pid
		for _, s := range ss {
			if s.Root == nil {
				continue
			}
			surfaceIDs = append(surfaceIDs, s.Surface)
			if s.Title != "" && title == pid {
				title = s.Title
			}
		}
		if len(surfaceIDs) == 0 {
			continue
		}
		sort.Strings(surfaceIDs)
		out = append(out, Section{
			ProviderID: pid,
			Title:      title,
			Surfaces:   surfaceIDs,
		})
	}
	return out
}

// SectionsFromComposedRoot derives sections from a composed ViewNode
// tree whose children are "provider/{id}/{surface}" section nodes
// (internal/ui.Compose). Multiple surfaces of the same provider collapse
// into one head section so N providers → N sections.
func SectionsFromComposedRoot(root provider.ViewNode) []Section {
	// Group surface children by provider id, preserving first-seen title.
	type acc struct {
		title    string
		surfaces []string
	}
	by := make(map[string]*acc)
	var order []string
	for _, child := range root.Children {
		pid, surface, ok := parseSectionID(child.ID)
		if !ok {
			continue
		}
		a, exists := by[pid]
		if !exists {
			a = &acc{title: pid}
			by[pid] = a
			order = append(order, pid)
		}
		if surface != "" {
			a.surfaces = append(a.surfaces, surface)
		}
		// Section title is the first text child with font=caption (compose).
		if a.title == pid {
			if t := sectionTitle(child); t != "" {
				a.title = t
			}
		}
	}
	sort.Strings(order)
	out := make([]Section, 0, len(order))
	for _, pid := range order {
		a := by[pid]
		sort.Strings(a.surfaces)
		out = append(out, Section{
			ProviderID: pid,
			Title:      a.title,
			Surfaces:   a.surfaces,
		})
	}
	return out
}

// parseSectionID splits "provider/{pid}/{surface}" as written by ui.Compose.
func parseSectionID(id string) (pid, surface string, ok bool) {
	const prefix = "provider/"
	if !strings.HasPrefix(id, prefix) {
		return "", "", false
	}
	rest := id[len(prefix):]
	i := strings.IndexByte(rest, '/')
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// sectionTitle reads the caption title node Compose attaches as child[0].
func sectionTitle(section provider.ViewNode) string {
	if len(section.Children) == 0 {
		return ""
	}
	t, _ := section.Children[0].Props["text"].(string)
	return t
}

// ComposeAndSections is a convenience: compose then derive head sections.
// Used by the server API and hermetic oracles that start from surfaces.
func ComposeAndSections(surfaces map[string][]provider.UISurface) (provider.ViewNode, []Section) {
	root := ui.Compose(surfaces)
	return root, SectionsFromSurfaces(surfaces)
}

func cloneModel(m MenuModel) MenuModel {
	out := MenuModel{Connected: m.Connected}
	if len(m.Sections) > 0 {
		out.Sections = make([]Section, len(m.Sections))
		copy(out.Sections, m.Sections)
		for i := range out.Sections {
			if len(out.Sections[i].Surfaces) > 0 {
				s := make([]string, len(out.Sections[i].Surfaces))
				copy(s, out.Sections[i].Surfaces)
				out.Sections[i].Surfaces = s
			}
		}
	}
	return out
}

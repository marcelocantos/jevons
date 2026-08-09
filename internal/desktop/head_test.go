// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package desktop_test

// 🎯T27.7 acceptance oracle (1): N enabled providers → N sections;
// toggling a provider makes its section appear/disappear — asserted
// against the model, not by eye.

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/desktop"
	"github.com/marcelocantos/jevons/internal/provider"
	"github.com/marcelocantos/jevons/internal/ui"
)

func surface(pid, sid, title string) provider.UISurface {
	return provider.UISurface{
		Surface: sid,
		Title:   title,
		Root: &provider.ViewNode{
			Type:  "text",
			ID:    "body",
			Props: map[string]any{"text": title},
		},
	}
}

func TestNProvidersNSections(t *testing.T) {
	surfaces := map[string][]provider.UISurface{
		"alpha": {surface("alpha", "alpha.panel", "Alpha")},
		"beta":  {surface("beta", "beta.panel", "Beta")},
		"gamma": {surface("gamma", "gamma.panel", "Gamma")},
	}
	secs := desktop.SectionsFromSurfaces(surfaces)
	if len(secs) != 3 {
		t.Fatalf("want 3 sections, got %d: %+v", len(secs), secs)
	}
	// Deterministic order by provider id.
	if secs[0].ProviderID != "alpha" || secs[1].ProviderID != "beta" || secs[2].ProviderID != "gamma" {
		t.Fatalf("order = %v", []string{secs[0].ProviderID, secs[1].ProviderID, secs[2].ProviderID})
	}
	if secs[0].Title != "Alpha" {
		t.Errorf("alpha title = %q", secs[0].Title)
	}
}

func TestMultiSurfaceSameProviderOneSection(t *testing.T) {
	// Acceptance: N providers → N sections, not N surfaces.
	surfaces := map[string][]provider.UISurface{
		"mnemo": {
			surface("mnemo", "mnemo.threads", "Threads"),
			surface("mnemo", "mnemo.health", "Health"),
		},
	}
	secs := desktop.SectionsFromSurfaces(surfaces)
	if len(secs) != 1 {
		t.Fatalf("want 1 section for multi-surface provider, got %d", len(secs))
	}
	if len(secs[0].Surfaces) != 2 {
		t.Fatalf("surfaces = %v", secs[0].Surfaces)
	}
}

func TestToggleProviderSectionAppearDisappear(t *testing.T) {
	h := desktop.NewHead()
	surfaces := map[string][]provider.UISurface{
		"mock": {surface("mock", "mock.status", "mock")},
		"zeta": {surface("zeta", "zeta.panel", "zeta")},
	}
	h.ApplySurfaces(surfaces)
	if got := len(h.Sections()); got != 2 {
		t.Fatalf("initial sections = %d, want 2", got)
	}

	// Toggle zeta off: remove from aggregated model.
	delete(surfaces, "zeta")
	h.ApplySurfaces(surfaces)
	secs := h.Sections()
	if len(secs) != 1 || secs[0].ProviderID != "mock" {
		t.Fatalf("after disable zeta: %+v", secs)
	}

	// Toggle zeta back on.
	surfaces["zeta"] = []provider.UISurface{surface("zeta", "zeta.panel", "zeta")}
	h.ApplySurfaces(surfaces)
	if got := len(h.Sections()); got != 2 {
		t.Fatalf("after re-enable zeta: %d sections", got)
	}
}

func TestSectionsFromComposedRootMatchesSurfaces(t *testing.T) {
	surfaces := map[string][]provider.UISurface{
		"mock": {surface("mock", provider.MockSurfaceName, "mock")},
		"zeta": {surface("zeta", "zeta.panel", "zeta")},
	}
	root := ui.Compose(surfaces)
	fromRoot := desktop.SectionsFromComposedRoot(root)
	fromSurf := desktop.SectionsFromSurfaces(surfaces)
	if len(fromRoot) != len(fromSurf) {
		t.Fatalf("root %d vs surfaces %d", len(fromRoot), len(fromSurf))
	}
	for i := range fromRoot {
		if fromRoot[i].ProviderID != fromSurf[i].ProviderID {
			t.Errorf("[%d] root=%q surf=%q", i, fromRoot[i].ProviderID, fromSurf[i].ProviderID)
		}
	}
}

func TestEmptySurfacesNoSections(t *testing.T) {
	if secs := desktop.SectionsFromSurfaces(nil); len(secs) != 0 {
		t.Fatalf("nil → %v", secs)
	}
	// Surface without Root is not renderable.
	secs := desktop.SectionsFromSurfaces(map[string][]provider.UISurface{
		"ghost": {{Surface: "ghost.x", Title: "Ghost"}},
	})
	if len(secs) != 0 {
		t.Fatalf("rootless → %v", secs)
	}
}

func TestHeadMarkConnected(t *testing.T) {
	h := desktop.NewHead()
	if h.Model().Connected {
		t.Fatal("fresh head should not be connected")
	}
	h.MarkConnected()
	if !h.Model().Connected {
		t.Fatal("MarkConnected did not stick")
	}
}

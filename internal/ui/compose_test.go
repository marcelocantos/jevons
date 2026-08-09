// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ui_test

// 🎯T27.6 acceptance oracle, composer half: the reference mock provider
// (T27.1) composes into the expected ViewNode tree, asserted against a
// golden fixture; a second provider composes alongside it with no
// per-provider code anywhere (Go producer is generic; iOS Swift sources
// are grep-checked for provider-specific identifiers); and every prop
// key in a composed tree stays within the pinned Swift ViewProps
// vocabulary so the T9/T11 renderer decodes it without an app rebuild.

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/provider"
	"github.com/marcelocantos/jevons/internal/ui"
)

var update = flag.Bool("update", false, "rewrite golden fixtures")

func composedMock(t *testing.T) *provider.Registry {
	t.Helper()
	reg := provider.NewRegistry()
	if err := reg.Register(provider.NewMock()); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestComposeGoldenMockTree(t *testing.T) {
	reg := composedMock(t)
	root := ui.Compose(reg.ComposedUI())
	got, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "composed_mock.golden.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("composed tree drifted from golden %s\n--- got ---\n%s", golden, got)
	}
}

func TestComposeSecondProviderComposesBoth(t *testing.T) {
	reg := composedMock(t)
	reg.SetSurfaces("zeta", []provider.UISurface{{
		Surface: "zeta.panel",
		Title:   "zeta",
		Root: &provider.ViewNode{
			Type: "vstack",
			ID:   "panel",
			Children: []provider.ViewNode{{
				Type:  "button",
				ID:    "poke",
				Props: map[string]any{"text": "Poke", "action": "poke"},
			}},
		},
	}})

	root := ui.Compose(reg.ComposedUI())
	if len(root.Children) != 2 {
		t.Fatalf("want 2 sections, got %d", len(root.Children))
	}
	// Deterministic order: provider ids sorted (mock < zeta).
	if root.Children[0].ID != "provider/mock/"+provider.MockSurfaceName {
		t.Errorf("first section = %q", root.Children[0].ID)
	}
	zeta := root.Children[1]
	if zeta.ID != "provider/zeta/zeta.panel" {
		t.Fatalf("second section = %q", zeta.ID)
	}
	// Subtree ids are provider-prefixed; actions are surface-namespaced.
	panel := zeta.Children[1]
	if panel.ID != "zeta/panel" {
		t.Errorf("panel id = %q", panel.ID)
	}
	btn := panel.Children[0]
	if btn.ID != "zeta/poke" {
		t.Errorf("button id = %q", btn.ID)
	}
	if got := btn.Props["action"]; got != "provider/zeta/zeta.panel/poke" {
		t.Errorf("action = %v", got)
	}
	// The mock's own subtree is untouched by the addition.
	mockBtn := root.Children[0].Children[1].Children[1]
	if got := mockBtn.Props["action"]; got != "provider/mock/mock.status/refresh" {
		t.Errorf("mock action = %v", got)
	}
}

func TestParseAction(t *testing.T) {
	pid, surface, name, ok := ui.ParseAction("provider/mock/mock.status/refresh")
	if !ok || pid != "mock" || surface != "mock.status" || name != "refresh" {
		t.Errorf("got (%q,%q,%q,%v)", pid, surface, name, ok)
	}
	// Nested action names keep their tail intact.
	_, _, name, ok = ui.ParseAction("provider/p/s/open/deep")
	if !ok || name != "open/deep" {
		t.Errorf("nested name = %q ok=%v", name, ok)
	}
	for _, bad := range []string{"send_message", "provider/x", "provider/x/y", "provider///", "other/a/b/c", ""} {
		if _, _, _, ok := ui.ParseAction(bad); ok {
			t.Errorf("ParseAction(%q) unexpectedly ok", bad)
		}
	}
}

// viewPropsVocab mirrors the CodingKeys of ios/Jevon/Models/ViewNode.swift
// ViewProps — the pinned client vocabulary (contract §6.1). Composed
// trees must not invent prop keys the shipped renderer cannot decode.
var viewPropsVocab = map[string]bool{
	"text": true, "placeholder": true, "sf_symbol": true, "image_asset": true, "image_url": true,
	"font": true, "weight": true,
	"color": true, "bg_color": true, "corner_radius": true, "opacity": true,
	"spacing": true, "padding": true, "min_length": true, "alignment": true, "max_lines": true, "truncate": true,
	"title": true, "title_display_mode": true,
	"disabled": true,
	"action":   true, "style": true,
	"keyboard": true, "autocorrect": true, "autocapitalize": true, "submit_label": true,
	"scroll_anchor": true, "scroll_dismiss_keyboard": true, "keyboard_avoidance": true,
	"frame_width": true, "frame_height": true, "frame_max_width": true, "frame_max_height": true,
	"foreground_style": true, "content_mode": true,
	"a11y_label": true,
}

func TestComposedTreeStaysInClientVocabulary(t *testing.T) {
	reg := composedMock(t)
	var walk func(n provider.ViewNode)
	walk = func(n provider.ViewNode) {
		for k := range n.Props {
			if !viewPropsVocab[k] {
				t.Errorf("node %q uses prop %q outside the Swift ViewProps vocabulary", n.ID, k)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(ui.Compose(reg.ComposedUI()))
}

func TestNoPerProviderSwift(t *testing.T) {
	// Load-bearing (acceptance 1): adding a provider must not require
	// Swift changes. The client renders the composed tree generically,
	// so no Swift source may reference provider-specific identifiers.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	iosDir := filepath.Join(filepath.Dir(file), "..", "..", "ios")
	forbidden := []string{provider.MockID + ".", "mnemo", "bullseye"}
	err := filepath.WalkDir(iosDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".swift") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(body)
		for _, f := range forbidden {
			if strings.Contains(src, f) {
				t.Errorf("%s references provider-specific identifier %q", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

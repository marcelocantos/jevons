// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

func TestReactPaintRefusesDailyAndNamesTargets(t *testing.T) {
	root, err := j19RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "scripts", "journey-suite", "react_paint.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	if !strings.Contains(src, "13705") {
		t.Error("react_paint.js must refuse daily :13705")
	}
	for _, id := range []string{"T279", "T281", "T504", "T106", "T59", "T238", "T126", "T123", "T72.1", "T250", "T185", "T390"} {
		if !strings.Contains(src, id) {
			t.Errorf("react_paint.js must name retired id %s (ledger referent, not current React)", id)
		}
	}
	if !strings.Contains(src, ".msg-clipped") {
		t.Error("T106 referent is vanilla .msg-clipped, not a React-only class")
	}
	if !strings.Contains(src, "#input") {
		t.Error("composer referent is vanilla #input")
	}
	for _, leak := range []string{
		`[data-composer`,
		`[data-frontier-graph]`,
		`[data-agent]`,
		`#viz-panel`,
		`#frontier-graph-panel`,
	} {
		if strings.Contains(src, leak) {
			t.Errorf("react_paint.js must not accept React-shaped fallback %q — referent is vanilla chrome", leak)
		}
	}
}

func TestReactSurfaceHelperExists(t *testing.T) {
	if err := portguard.RefuseDaily(13705); err == nil {
		t.Fatal("RefuseDaily(13705) must error")
	}
	root, err := j19RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "scripts", "journey-suite", "react_surface.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "startReactSurface") {
		t.Error("react_surface.go must export startReactSurface for the chrome pack")
	}
	if !strings.Contains(string(src), "T540.2") {
		t.Error("must name the vanilla GET / residual")
	}
}

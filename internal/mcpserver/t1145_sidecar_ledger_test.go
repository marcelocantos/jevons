// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
)

func TestStitchAgentStartSetsSidecarLedgerProtectedPaths(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	def, _, _, err := s.stitchAgentStart(
		"jv-t1145-protected",
		t.TempDir(),
		"",
		string(claudia.ProviderCursor),
		"",
		"jevons-po",
		claudia.PurposeWork,
		"",
		"",
	)
	if err != nil {
		t.Fatalf("stitchAgentStart: %v", err)
	}
	want := fleet.SidecarLedgerProtectedPaths()
	if len(def.ProtectedPaths) != len(want) {
		t.Fatalf("ProtectedPaths len %d want %d", len(def.ProtectedPaths), len(want))
	}
	if def.ProtectedPaths[0].Pattern != fleet.LedgerProtectedPathGlob {
		t.Fatalf("pattern %q want %q", def.ProtectedPaths[0].Pattern, fleet.LedgerProtectedPathGlob)
	}
	if def.ProtectedPaths[0].Reason == "" {
		t.Fatal("expected T546 reason on protected path rule")
	}
}

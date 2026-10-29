// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cost"
)

// 🎯T358: owner-ratified po_fable pins Claude Fable on PO mint.
func TestT358POMintFableWhenRatified(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderClaude))
	s.SetFableClassRatified(cost.FableRatifiedPOFable)

	s.mu.Lock()
	s.pendingSpawnRole = purposeProductOwner
	s.mu.Unlock()
	po, _, note, err := s.stitchAgentStart(
		"jevons-po", t.TempDir(), "", string(claudia.ProviderClaude), "",
		"jevons", claudia.PurposeWork, "", "",
	)
	s.mu.Lock()
	s.pendingSpawnRole = ""
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if po.Model != cost.ModelClaudeFableClass {
		t.Fatalf("model=%q want %s", po.Model, cost.ModelClaudeFableClass)
	}
	if !strings.Contains(note, "fable_class_default") {
		t.Fatalf("start note missing knob: %q", note)
	}
}

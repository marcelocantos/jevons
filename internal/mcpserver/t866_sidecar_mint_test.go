// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestStitchOmitProviderClaudeBecomesAnthropic(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderClaude))

	def, _, _, err := s.stitchAgentStart(
		"jv-t866-claude", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.Provider("anthropic") {
		t.Fatalf("omit-provider claude mint → %q, want anthropic", def.Provider)
	}
}

func TestStitchExplicitCursorStaysCursor(t *testing.T) {
	// 🎯T1013.3: cursor is genuinely unsteerable per claudia (claudia
	// T118's open tool-budget defect); this test's subject is explicit-pin
	// stability across the stitch path, not steerability, so it forces the
	// synthetic empty table like every other non-steerability T791 test.
	t791Steerable(t)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	def, _, _, err := s.stitchAgentStart(
		"jv-t866-cursor", t.TempDir(), "", string(claudia.ProviderCursor), "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderCursor {
		t.Fatalf("cursor mint → %q", def.Provider)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// TestT5363DoctrinePointsAtScoutCycle ratchets 🎯T536.3: instruction
// files require fog-of-war scout before implement and point at
// internal/envelope rather than inventing a parallel schema.
func TestT5363DoctrinePointsAtScoutCycle(t *testing.T) {
	need := []string{
		"🎯T536.3",
		"scout-report",
		"phase scout",
		"InheritLedger",
		"internal/envelope",
	}
	docs := []string{
		"agents-guide.md",
		"AGENTS.md",
		"internal/config/persona.md",
		"internal/mcpserver/fleet_brief.go",
	}
	for _, doc := range docs {
		body := readRepo(t, doc)
		for _, m := range need {
			if !strings.Contains(body, m) {
				t.Errorf("%s missing T536.3 marker %q", doc, m)
			}
		}
	}
}

func TestT5363SchemaOwnsScoutVocabulary(t *testing.T) {
	body := readRepo(t, "internal/envelope/kind.go")
	if !strings.Contains(body, "KindScoutReport") || !strings.Contains(body, "scout-report") {
		t.Error("internal/envelope/kind.go missing KindScoutReport")
	}
	phase := readRepo(t, "internal/envelope/phase.go")
	for _, want := range []string{"PhaseScout", "PhaseImplement", "ParsePhase", "EffectivePhase"} {
		if !strings.Contains(phase, want) {
			t.Errorf("phase.go missing %q", want)
		}
	}
	fog := readRepo(t, "internal/envelope/fog.go")
	for _, want := range []string{"InheritLedger", "MayImplementAfterScout", "FogMap", "fog-known"} {
		// fog-known is slot name in parse.go, not fog.go — check parse for slots.
		if want == "fog-known" {
			continue
		}
		if !strings.Contains(fog, want) {
			t.Errorf("fog.go missing %q", want)
		}
	}
	parse := readRepo(t, "internal/envelope/parse.go")
	for _, want := range []string{"fog-known", "fog-unknown", "fog-blindspot", "phase"} {
		if !strings.Contains(parse, want) {
			t.Errorf("parse.go missing fog/phase slot %q", want)
		}
	}
	// Docs must name scout-report as a real kind (AllKinds owns the list).
	found := false
	for _, k := range envelope.AllKinds() {
		if k == envelope.KindScoutReport {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("KindScoutReport not in AllKinds")
	}
}

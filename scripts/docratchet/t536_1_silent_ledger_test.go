// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// TestT5361DoctrinePointsAtSilentLedger ratchets 🎯T536.1: instruction
// files require the silent-decision ledger and point at internal/envelope
// rather than inventing a parallel schema.
func TestT5361DoctrinePointsAtSilentLedger(t *testing.T) {
	need := []string{
		"🎯T536.1",
		"silent-ledger",
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
				t.Errorf("%s missing T536.1 marker %q", doc, m)
			}
		}
	}
}

func TestT5361SchemaOwnsSilentLedgerVocabulary(t *testing.T) {
	body := readRepo(t, "internal/envelope/ledger.go")
	for _, want := range []string{
		"SilentLedgerEmpty",
		"SilentLedgerRanked",
		"ReadSilentLedger",
		"MissingSilentLedger",
		"least-confident",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("internal/envelope/ledger.go missing %q", want)
		}
	}
	// Docs must not invent a silent-ledger value outside the schema.
	for _, doc := range []string{"agents-guide.md", "AGENTS.md"} {
		body := readRepo(t, doc)
		if strings.Contains(body, "silent-ledger") &&
			!strings.Contains(body, "silent-ledger none") &&
			!strings.Contains(body, "silent-ledger ranked") {
			t.Errorf("%s mentions silent-ledger without none|ranked examples", doc)
		}
	}
	_ = envelope.SilentLedgerEmpty // keep import for package identity
}

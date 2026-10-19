// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/guardreach"
)

// Doctrine surfaces that must name 🎯T419 so the next author who adds a
// silent-failure guard reaches for call-site reachability rather than a
// lone unit test of the guard function.
var t419DoctrineFiles = []string{
	"AGENTS.md",
	"agents-guide.md",
	"internal/config/persona.md",
	"internal/mcpserver/fleet_brief.go",
}

// TestT419DoctrineAndCatalogue is the standing enforcement point (🎯T419
// acceptance 4): catalogue check against this tree plus doctrine tokens.
func TestT419DoctrineAndCatalogue(t *testing.T) {
	root := repoRoot(t)
	for _, f := range guardreach.CheckRepo(root) {
		t.Errorf("catalogue: %s", f)
	}

	need := []string{
		"T419",
		"production call site",
		"MarkDelivered", // pinning instance named so the story is not lost
	}
	for _, doc := range t419DoctrineFiles {
		body := readRepo(t, doc)
		for _, m := range need {
			if !hasWholeToken(body, m) && !strings.Contains(body, m) {
				t.Errorf("%s missing 🎯T419 marker %q", doc, m)
			}
		}
		// Accept either spelling of the core distinction.
		if !strings.Contains(body, "production call site") &&
			!strings.Contains(body, "through its production") &&
			!strings.Contains(body, "call-site reachability") {
			t.Errorf("%s missing call-site reachability gloss (🎯T419)", doc)
		}
	}

	if len(guardreach.AuditOnly()) == 0 {
		t.Fatal("🎯T419 clause 3: audit residuals must remain listable")
	}
}

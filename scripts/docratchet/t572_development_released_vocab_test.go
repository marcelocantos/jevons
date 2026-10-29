// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT572InstructionSurfaces ratchets 🎯T572: two environments only
// (development / released), no forbidden product phrases on instruction surfaces.
func TestT572InstructionSurfaces(t *testing.T) {
	banned := []string{
		"daily path",
		"daily cockpit",
		"daily build",
		"daily driver",
		"daily API",
		"daily daemon",
		"Universe A (daily)",
		"daily jevonsd",
	}
	for _, path := range []string{
		"internal/config/persona.md",
		"AGENTS.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		body := readRepo(t, path)
		if !strings.Contains(body, "development") || !strings.Contains(body, "released") {
			t.Errorf("%s missing development/released vocabulary", path)
		}
		if !strings.Contains(body, "T572") && !strings.Contains(body, "🎯T572") {
			t.Errorf("%s missing T572 marker", path)
		}
		for _, phrase := range banned {
			if strings.Contains(body, phrase) {
				t.Errorf("%s reminted forbidden phrase %q", path, phrase)
			}
		}
	}
	for _, path := range []string{
		"internal/config/persona.md",
		"AGENTS.md",
		"agents-guide.md",
	} {
		body := readRepo(t, path)
		for _, legacy := range []string{"HasDailyPathEvidence", "DailyPort"} {
			if !strings.Contains(body, legacy) {
				t.Errorf("%s missing legacy identifier gloss %q", path, legacy)
			}
		}
	}
}

// TestT572OwnerFacingCopy ratchets owner-visible daemon / desktop copy.
func TestT572OwnerFacingCopy(t *testing.T) {
	banned := []string{
		"daily cockpit",
		"daily seat",
		"daily jevonsd",
		"daily API",
	}
	for _, path := range []string{
		"supervisor/jevonsd.ini",
		"internal/desktop/connect.go",
	} {
		body := readRepo(t, path)
		for _, phrase := range banned {
			if strings.Contains(strings.ToLower(body), phrase) {
				t.Errorf("%s contains forbidden owner-facing phrase %q", path, phrase)
			}
		}
	}
}

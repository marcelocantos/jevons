// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestDevelopmentSurfaceAchieveDoctrineMarkers ratchets 🎯T552 / 🎯T553.2 (was T194):
// owner-visible behaviour is observed on the running development surface.
// The restart script and HasActivationEvidence are activation seams, not the gate.
func TestDevelopmentSurfaceAchieveDoctrineMarkers(t *testing.T) {
	persona := readRepo(t, "internal/config/persona.md")
	agents := readRepo(t, "AGENTS.md")
	guide := readRepo(t, "agents-guide.md")
	brief := readRepo(t, "internal/mcpserver/fleet_brief.go")

	for _, doc := range []struct {
		name, body string
		need       []string
	}{
		{"internal/config/persona.md", persona, []string{
			"🎯T194",
			"🎯T552",
			"🎯T553.2",
			"necessary, not sufficient",
			"restart-jevonsd",
			"live probe",
			"stale binary",
			"HasActivationEvidence",
			"Hermetic unit green",
			"not an achieve gate",
		}},
		{"AGENTS.md", agents, []string{
			"🎯T194",
			"🎯T552",
			"necessary not sufficient",
			"restart-jevonsd",
			"live probe",
			"HasActivationEvidence",
			"stale binary",
			"not an achieve gate",
		}},
		{"agents-guide.md", guide, []string{
			"🎯T194",
			"🎯T552",
			"necessary not sufficient",
			"restart-jevonsd",
			"live probe",
			"HasActivationEvidence",
			"hermetics alone",
		}},
		{"internal/mcpserver/fleet_brief.go", brief, []string{
			"🎯T194",
			"🎯T552",
			"necessary not sufficient",
			"restart-jevonsd",
			"live probe",
			"HasActivationEvidence",
			"hermetics alone",
			"stale binary",
		}},
	} {
		for _, m := range doc.need {
			if !strings.Contains(doc.body, m) {
				t.Errorf("%s missing doctrine marker %q", doc.name, m)
			}
		}
	}
}

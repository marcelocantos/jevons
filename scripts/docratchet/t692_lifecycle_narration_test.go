// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestLifecycleNarrationDoctrineMarkers ratchets 🎯T692: fleet state
// narration matches the live registry. Doctrine names the specimen
// (claiming a running seat is dead) and the classifier helpers.
func TestLifecycleNarrationDoctrineMarkers(t *testing.T) {
	agents := readRepo(t, "AGENTS.md")
	guide := readRepo(t, "agents-guide.md")
	brief := readRepo(t, "internal/mcpserver/fleet_brief.go")

	need := []string{
		"🎯T692",
		"Fleet state narration matches the live registry",
		"killing",
		"will stop",
		"killed",
		"is stopped",
		"jv-t679.2-born-stuck",
		"claiming a running seat is dead",
		"LooksLikeUnverifiedLifecycleClaim",
		"ClassifyLifecycleNarration",
		"jevons_agent_list",
		"GET /api/agents",
	}
	overseerRole := readRepo(t, "internal/roles/builtin/overseer.md")
	for _, doc := range []struct {
		name, body string
	}{
		{"internal/roles/builtin/overseer.md", overseerRole},
		{"AGENTS.md", agents},
		{"agents-guide.md", guide},
		{"internal/mcpserver/fleet_brief.go", brief},
	} {
		for _, m := range need {
			if !strings.Contains(doc.body, m) {
				t.Errorf("%s missing T692 doctrine marker %q", doc.name, m)
			}
		}
	}
}

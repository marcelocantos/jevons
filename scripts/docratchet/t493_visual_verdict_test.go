// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestVisualCockpitProseVerdictDoctrineMarkers ratchets 🎯T493.1: after
// visual cockpit work the agent answers in prose whether the screenshot
// looks like a normal transcript. A green metric is not that answer.
func TestVisualCockpitProseVerdictDoctrineMarkers(t *testing.T) {
	persona := readRepo(t, "internal/config/persona.md")
	agents := readRepo(t, "AGENTS.md")
	guide := readRepo(t, "agents-guide.md")
	brief := readRepo(t, "internal/mcpserver/fleet_brief.go")

	for _, doc := range []struct {
		name, body string
		need       []string
	}{
		{"internal/config/persona.md", persona, []string{
			"🎯T493.1",
			"Visual cockpit finish is a prose look, not a green metric",
			"#messages",
			"normal chat transcript after a hard reload",
			"visibleInScroller",
			"screenshot-tool caption",
			"automatic no",
			"HasVisualProseVerdict",
			"LooksLikeMissingVisualVerdict",
		}},
		{"AGENTS.md", agents, []string{
			"🎯T493.1",
			"Visual cockpit finish is a prose look, not a green metric",
			"#messages",
			"normal chat transcript after a hard reload",
			"visibleInScroller",
			"screenshot-tool caption",
			"automatic no",
			"HasVisualProseVerdict",
			"LooksLikeMissingVisualVerdict",
		}},
		{"agents-guide.md", guide, []string{
			"🎯T493.1",
			"Visual cockpit finish is a prose look, not a green metric",
			"#messages",
			"normal chat transcript after a hard reload",
			"visibleInScroller",
			"screenshot-tool caption",
			"automatic no",
			"HasVisualProseVerdict",
			"LooksLikeMissingVisualVerdict",
		}},
		{"internal/mcpserver/fleet_brief.go", brief, []string{
			"🎯T493.1",
			"Visual cockpit finish is a prose look, not a green metric",
			"#messages",
			"normal chat transcript after a hard reload",
			"visibleInScroller",
			"screenshot-tool caption",
			"automatic no",
			"HasVisualProseVerdict",
			"LooksLikeMissingVisualVerdict",
		}},
	} {
		for _, m := range doc.need {
			if !strings.Contains(doc.body, m) {
				t.Errorf("%s missing doctrine marker %q", doc.name, m)
			}
		}
	}
}

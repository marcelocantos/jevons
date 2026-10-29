// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

func TestT1151_6SiblingReplaceDoctrine(t *testing.T) {
	for _, rel := range []string{
		"AGENTS.md",
		"internal/config/persona.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		lower := strings.ToLower(readRepo(t, rel))
		if !strings.Contains(lower, "🎯t1151.6") {
			t.Errorf("%s no longer cites 🎯T1151.6", rel)
			continue
		}
		for _, m := range []string{
			"needs-release",
			"active-wait",
			"does not edit that",
			"sibling's shared clone",
			"go.mod replace",
		} {
			if !strings.Contains(lower, m) {
				t.Errorf("%s: missing doctrine marker %q", rel, m)
			}
		}
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

func TestT1134LandingNeverStashesDoctrine(t *testing.T) {
	for _, rel := range []string{
		"AGENTS.md",
		"internal/config/persona.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		lower := strings.ToLower(readRepo(t, rel))
		if !strings.Contains(lower, "🎯t1134") {
			t.Errorf("%s no longer cites 🎯T1134", rel)
			continue
		}
		for _, m := range []string{
			"never run `git stash`",
			"git checkout --",
			"cmd/integrate",
			"never stashes",
		} {
			if !strings.Contains(lower, m) {
				t.Errorf("%s: missing doctrine marker %q", rel, m)
			}
		}
	}
}

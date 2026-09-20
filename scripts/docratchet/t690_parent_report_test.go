// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT690DoctrineNamesDaemonParentReport ratchets 🎯T690: instruction
// files name the daemon parent-report channel so a denied
// jevons_agent_send is not read as "the parent cannot hear you".
func TestT690DoctrineNamesDaemonParentReport(t *testing.T) {
	need := []string{
		"🎯T690",
		"parent_report: daemon-delivered",
		"jevons_agent_send",
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
				t.Errorf("%s missing T690 marker %q", doc, m)
			}
		}
	}
}

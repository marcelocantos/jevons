// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

func TestT1129OwnerChatDoctrineMarkers(t *testing.T) {
	need := []string{
		"🎯T1129",
		"make test-ui-live",
		"🎯T572",
		"owner cockpit session",
		"JevonsFleetBrowser",
		"type into or submit",
	}
	for _, doc := range []struct{ name, body string }{
		{"agents-guide.md", readRepo(t, "agents-guide.md")},
		{"AGENTS.md", readRepo(t, "AGENTS.md")},
		{"internal/config/persona.md", readRepo(t, "internal/config/persona.md")},
		{"internal/mcpserver/fleet_brief.go", readRepo(t, "internal/mcpserver/fleet_brief.go")},
	} {
		for _, m := range need {
			if !strings.Contains(doc.body, m) {
				t.Errorf("%s missing doctrine marker %q", doc.name, m)
			}
		}
	}
}

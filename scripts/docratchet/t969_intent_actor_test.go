// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT969FleetIntentActorDoctrineMarkers ratchets 🎯T969: a fleet-intent
// change records who actually made it, and no agent reverses or confesses to
// one without reading that record. On 2026-09-30 an actor-less resume was
// recorded "by jevons" and jevons-po stopped five workers confessing to it.
func TestT969FleetIntentActorDoctrineMarkers(t *testing.T) {
	need := []string{
		"🎯T969",
		"Fleet intent records who decided",
		"read the recorded actor and reason",
		"not reversed without an",
		"never",
		"confess to a change",
		"actor=",
		"jevons_agent_stop",
		"jevons_agent_kill",
		"client:<program>",
		"unattributed",
		"never as the overseer",
		"GET /api/fleet-intent",
	}
	for _, path := range []string{
		"internal/config/persona.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		// Line wrapping is layout, not doctrine: compare on single spaces.
		body := strings.Join(strings.Fields(readRepo(t, path)), " ")
		for _, m := range need {
			if !strings.Contains(body, m) {
				t.Errorf("%s missing T969 doctrine marker %q", path, m)
			}
		}
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT1245POProviderReroute pins the full instruction on every standing surface.
func TestT1245POProviderReroute(t *testing.T) {
	const doctrine = "When a seat's provider cannot run a target's gate for an environmental reason (sandbox denial, provider outage or wedge, or exhausted plan window), the PO picks another provider itself, preferring one whose plan window is not exhausted per /api/plan-usage, and reports the choice. Do not park for the owner to name a provider. This is Build-plane execution placement, not a waiver or scope change. Specimen: claudia-po turns 250-253 (2026-10-28) held the Claudia release asking the owner to name Anthropic or Cursor after Codex's sandbox denied nested sandbox-exec; that is the bad shape."
	for _, file := range []string{"internal/config/persona.md", "agents-guide.md", "internal/cli/help_agent.md", "internal/mcpserver/fleet_brief.go"} {
		body := readRepo(t, file)
		if !strings.Contains(strings.Join(strings.Fields(body), " "), "## PO provider reroute for environmental gate failure (🎯T1245) "+doctrine) {
			t.Errorf("%s: missing identical T1245 provider reroute doctrine", file)
		}
	}
}

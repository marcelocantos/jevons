// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT1262POReplyGateDoctrine pins the rule that only answers and typed
// envelopes from a product owner reach the overseer, on every standing
// surface a PO reads.
func TestT1262POReplyGateDoctrine(t *testing.T) {
	const heading = "## What reaches the overseer from a PO (🎯T1262)"
	for _, file := range []string{
		"internal/config/persona.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		body := readRepo(t, file)
		if !strings.Contains(body, heading) || !strings.Contains(body, "jevons: kind escalation") {
			t.Errorf("%s: missing the T1262 PO reply doctrine", file)
		}
	}
}

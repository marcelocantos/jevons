// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT1241POBuildAuthority pins the same complete instruction on all
// standing surfaces, including the CLI help mirror. A fragmentary keyword
// check would permit one surface to silently lose the owner-approval boundary.
func TestT1241POBuildAuthority(t *testing.T) {
	const doctrine = "A PO proceeds on its own authority for test-only fixes, gate re-runs, and repair plans on a Build-plane target it owns; delegate execution and independent verification to workers and auditors rather than doing them in the PO turn. Do not park that Build work asking the owner to GO. Owner approval is reserved for waivers, scope changes, credential-touching steps, and Ship-plane actions the owner has not authorised. Specimen: claudia-po turns 221-224 (2026-10-28) parked 🎯claudia/T228 for over an hour asking the owner to GO event-driven test waits and one clean gate; that is the bad shape. A build, Makefile, or test-harness change the target's own gate needs in order to pass is part of that Build work, not a scope change. Second specimen: claudia-po turns 234-241 parked the same target again, calling a Makefile target that installs the sidecar's frozen dependencies before the Go tests a scope change."
	for _, file := range []string{
		"internal/config/persona.md",
		"agents-guide.md",
		"internal/cli/help_agent.md",
		"internal/mcpserver/fleet_brief.go",
	} {
		body := readRepo(t, file)
		if !strings.Contains(strings.Join(strings.Fields(body), " "), "## PO Build-plane authority (🎯T1241) "+doctrine) {
			t.Errorf("%s: missing identical T1241 Build-plane authority doctrine", file)
		}
	}
}

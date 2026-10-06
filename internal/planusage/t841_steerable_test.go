// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "testing"

// 🎯T1013.3: UnsteerableReason is now computed by claudia
// (claudia.CapabilityMCPTools / claudia.Steerable), not read from a
// hand-maintained table in this package. These are the real-world
// answers as of this build: claude/codex/grok are steerable (codex's
// MCP-approval rejection was fixed at claudia 5fa7f35/v0.42.0, 🎯T841);
// cursor is not (claudia T118's cumulative tool-budget defect remains
// open/set_aside).
func TestUnsteerableReasonDelegatesToClaudia(t *testing.T) {
	for _, p := range []string{"codex", " Codex ", "claude", "grok"} {
		if got := UnsteerableReason(p); got != "" {
			t.Fatalf("%q must be steerable per claudia, got %q", p, got)
		}
	}
	if got := UnsteerableReason(" CURSOR "); got == "" {
		t.Fatal("cursor must report a reason: claudia T118 is open")
	}
}

// SetUnsteerableForTest is a pure test seam (overrides the lookup
// function, not a jevons-owned data table) so the mint-exclusion
// mechanism can be exercised against a synthetic provider set that does
// not depend on which providers claudia currently reports as
// unsteerable for real.
func TestSetUnsteerableForTestOverridesAndRestores(t *testing.T) {
	if got := UnsteerableReason("grok"); got != "" {
		t.Fatalf("grok must be steerable before the override, got %q", got)
	}
	restore := SetUnsteerableForTest(map[string]string{"grok": "synthetic"})
	if got := UnsteerableReason(" GROK "); got != "synthetic" {
		t.Fatalf("mechanism must refuse a named provider, got %q", got)
	}
	restore()
	if got := UnsteerableReason("grok"); got != "" {
		t.Fatalf("grok must be steerable again after restore, got %q", got)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "testing"

// 🎯T841: codex is steerable (claudia v0.42.0); only cursor stays excluded.
func TestT841SteerableTableListsOnlyCursor(t *testing.T) {
	if got := UnsteerableReason("cursor"); got != "claudia T118" {
		t.Fatalf("cursor reason = %q", got)
	}
	if got := UnsteerableReason(" Codex "); got != "" {
		t.Fatalf("codex must be steerable, got %q", got)
	}
	if len(unsteerable) != 1 {
		t.Fatalf("table = %v, want only cursor", unsteerable)
	}
}

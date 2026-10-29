// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// 🎯T836: each J21 backend must bind a distinct target_id so one arm cannot
// engage T510 for the others. T222 refusal on a genuinely engaged target stays
// in mcpserver.TestAgentStartRefusesSecondEngagement.
func TestJ21BackendTargetIDsDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, p := range []string{"claude", "grok", "codex"} {
		id := j21TargetID(p)
		if id == "T510" {
			t.Fatalf("%s still uses the shared product id", p)
		}
		if other, ok := seen[id]; ok {
			t.Fatalf("duplicate target_id %q for %s and %s", id, other, p)
		}
		seen[id] = p
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "testing"

// 🎯T841: no provider is excluded; the mechanism still refuses whatever a
// table names.
func TestT841SteerableTableIsEmpty(t *testing.T) {
	for _, p := range []string{"cursor", " Codex ", "claude", "grok"} {
		if got := UnsteerableReason(p); got != "" {
			t.Fatalf("%q must be steerable, got %q", p, got)
		}
	}
	if len(unsteerable) != 0 {
		t.Fatalf("table = %v, want empty", unsteerable)
	}
	defer SetUnsteerableForTest(map[string]string{"grok": "synthetic"})()
	if got := UnsteerableReason(" GROK "); got != "synthetic" {
		t.Fatalf("mechanism must refuse a named provider, got %q", got)
	}
}

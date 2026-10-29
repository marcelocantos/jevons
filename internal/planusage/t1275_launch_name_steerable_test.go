// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "testing"

// 🎯T1275: a provider named in the launch vocabulary is the same plan as its
// plan name, so it is exactly as steerable. Before the fix, openai-codex had
// no claudia capability row and an explicit openai-codex spawn was minted on
// claude (journey suite J9 and J17, 2026-10-29).
func TestT1275LaunchNamesAreSteerableAsTheirPlan(t *testing.T) {
	for _, p := range []string{"openai-codex", "anthropic", "xai-oauth", " OpenAI-Codex "} {
		if got := UnsteerableReason(p); got != "" {
			t.Fatalf("%q must be steerable like its plan, got %q", p, got)
		}
	}
}

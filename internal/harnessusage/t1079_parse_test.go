// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import "testing"

// 🎯T1079: a launch id names the same harness as its plan.
func TestT1079ParseHarnessReadsLaunchNames(t *testing.T) {
	for in, want := range map[string]Harness{
		"xai-oauth": HarnessGrok, "anthropic": HarnessClaude, "openai-codex": HarnessCodex,
		"grok": HarnessGrok, "claude-code": HarnessClaude, "acp": HarnessGrok, "openai": HarnessCodex,
	} {
		if got, err := ParseHarness(in); err != nil || got != want {
			t.Errorf("ParseHarness(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

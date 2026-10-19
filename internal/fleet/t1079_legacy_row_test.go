// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1079: every sidecar seat streams token fragments, whichever spelling
// its row stores. The separator table named grok and the launch ids, so a
// claude or codex row joined its reply's fragments with newlines.
func TestT1079ChunkSeparatorMatchesLaunchRow(t *testing.T) {
	for _, pair := range [][2]claudia.Provider{
		{"grok", "xai-oauth"}, {"claude", "anthropic"}, {"codex", "openai-codex"},
	} {
		if a, b := chunkSeparator(pair[0]), chunkSeparator(pair[1]); a != b || a != "" {
			t.Errorf("chunk separator: %s=%q %s=%q, want both empty", pair[0], a, pair[1], b)
		}
	}
	if CodexWorkSandbox("codex", claudia.PurposeWork, "") != CodexWorkSandbox("openai-codex", claudia.PurposeWork, "") {
		t.Error("codex work sandbox differs by spelling")
	}
}

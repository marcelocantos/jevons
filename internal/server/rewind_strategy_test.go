// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T214 J5: rewind path is provider-honest — Claude is not silently
// force-routed through Grok rotate-only policy as if it were ACP.
func TestRewindStrategyForProvider(t *testing.T) {
	cases := []struct {
		p    claudia.Provider
		want rewindStrategy
	}{
		{claudia.ProviderClaude, rewindNativeJSONL},
		{"", rewindNativeJSONL}, // claudia empty = Claude
		{claudia.ProviderGrok, rewindRotateRecap},
		{claudia.ProviderCodex, rewindRotateRecap},
		{claudia.ProviderBedrock, rewindRotateRecap},
		{claudia.Provider("custom"), rewindRotateRecap}, // unknown: no Claude JSONL rules
	}
	for _, tc := range cases {
		got := rewindStrategyForProvider(tc.p)
		if got != tc.want {
			t.Errorf("rewindStrategyForProvider(%q) = %v, want %v", tc.p, got, tc.want)
		}
	}
}

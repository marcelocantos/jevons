// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import "testing"

// 🎯T1079: the portfolio's fast-cheap peer is the plan's, whichever spelling
// names it. A launch id read as no harness at all.
func TestT1079FastCheapPeerMatchesLaunchName(t *testing.T) {
	for _, pair := range [][2]string{{"grok", "xai-oauth"}, {"codex", "openai-codex"}, {"claude", "anthropic"}} {
		if a, b := FastCheapPeer(pair[0]), FastCheapPeer(pair[1]); a != b {
			t.Errorf("fast-cheap peer: %s=%q %s=%q", pair[0], a, pair[1], b)
		}
	}
}

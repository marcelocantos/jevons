// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "strings"

// unsteerable is the steerability capability table (🎯T791): a provider
// listed here has seats that cannot receive jevons_* tools or accept
// steering, so plan headroom never makes it a mint destination. The value
// names why (typically the claudia target whose landing lifts the exclusion);
// add a row to exclude a provider, delete it to re-admit one. No provider is
// currently excluded (🎯T841): codex left with claudia v0.42.0, and cursor
// left once the boot scrub kept jevonsmcp first in ~/.cursor/mcp.json (the
// root cause of claudia T118). The mechanism stays; no other code names
// these providers.
var unsteerable = map[string]string{}

// UnsteerableReason returns why provider's seats cannot be steered, or ""
// when they can (or the provider is unknown).
func UnsteerableReason(provider string) string {
	return unsteerable[strings.ToLower(strings.TrimSpace(provider))]
}

// SetUnsteerableForTest swaps the capability table and returns a restore.
func SetUnsteerableForTest(m map[string]string) (restore func()) {
	old := unsteerable
	unsteerable = m
	return func() { unsteerable = old }
}

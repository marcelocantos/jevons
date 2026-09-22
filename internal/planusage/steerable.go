// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import "strings"

// unsteerable is the steerability capability table (🎯T791): a provider
// listed here has seats that cannot receive jevons_* tools or accept
// steering, so plan headroom never makes it a mint destination. The value
// names the claudia target whose landing lifts the exclusion — delete the
// row (or flip it via the claudia-side capability once published) and the
// provider is a destination again; no other code names these providers.
// Codex left the table when claudia v0.42.0 stopped rejecting its seats' MCP
// calls (🎯T841); only cursor (claudia T118) remains excluded.
var unsteerable = map[string]string{
	"cursor": "claudia T118",
}

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

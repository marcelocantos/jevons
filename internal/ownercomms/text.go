// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownercomms

import "strings"

// SuppressRoutine is a deliberately narrow server-side admission rule for
// unsolicited fleet-turn prose. It never decides whether an arbitrary message
// is safe: unknown text passes through. Owner-origin turns bypass this rule.
// The exception is checked before routine markers so a *hypothetical* safety
// incident in an otherwise routine report remains visible for investigation.
func SuppressRoutine(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, signal := range []string{"safety", "incident", "security", "breach", "unauthorized", "uncontained", "data loss", "risk", "failed", "failure", "broken", "stuck", "error", "decision needed", "owner decision", "owner:", "need your", "please decide", "blocked on owner"} {
		if strings.Contains(t, signal) {
			return false
		}
	}
	// Only text that explicitly describes an uneventful fleet report or a
	// no-action acknowledgment is suppressible. Do not classify by the mere
	// presence of a worker name, SHA, gate result or technical subject.
	for _, marker := range []string{"no action needed", "nothing to relay", "no new information", "already reviewed", "already covered", "routine progress", "routine worker", "worker finished", "worker completed", "green gate", "benign idle", "just the terminal marker"} {
		if strings.Contains(t, marker) {
			return true
		}
	}
	return false
}

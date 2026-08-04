// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package silentresponse classifies fleet-ops replies that must not surface
// to the owner (🎯T238 overseer→owner; mcpserver worker→overseer notify).
package silentresponse

import "strings"

// Prefix marks filterable ops replies. Case-insensitive on the filter.
const Prefix = "[silent]"

// Is reports whether agent completion text is owner-filterable (starts with
// [silent], optional leading whitespace / short multi-line lead-in).
func Is(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	prefix := strings.ToLower(Prefix)
	if strings.HasPrefix(lower, prefix) {
		return true
	}
	// Marker on its own line within a short lead-in (first 80 runes).
	head := t
	if len(head) > 80 {
		head = head[:80]
	}
	for _, line := range strings.Split(head, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), prefix) {
			return true
		}
	}
	return false
}

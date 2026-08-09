// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package silentresponse classifies fleet-ops replies that must not surface
// to the owner (🎯T238 overseer→owner; 🎯T240 whole-stream seal;
// mcpserver worker→overseer notify).
package silentresponse

import "strings"

// Prefix marks filterable ops replies. Case-insensitive on the filter.
const Prefix = "[silent]"

// StreamClass is the progressive classification of an accumulating assistant
// stream (🎯T240). ACP delivers token deltas; a single fragment rarely holds
// the full [silent] marker.
type StreamClass int

const (
	// Pending — not enough text yet to decide (empty or incomplete prefix).
	Pending StreamClass = iota
	// Silent — sealed or partial text is owner-filterable.
	Silent
	// Visible — cannot become silent (owner-visible prose).
	Visible
)

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

// Classify reports whether accumulating stream text is silent, still pending
// a complete [silent] prefix, or definitely owner-visible (🎯T240).
func Classify(acc string) StreamClass {
	if Is(acc) {
		return Silent
	}
	t := strings.TrimSpace(acc)
	if t == "" {
		return Pending
	}
	lower := strings.ToLower(t)
	pref := strings.ToLower(Prefix)
	// Incomplete marker: "[" / "[s" / "[silent" still Pending.
	if strings.HasPrefix(pref, lower) {
		return Pending
	}
	return Visible
}

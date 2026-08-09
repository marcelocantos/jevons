// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"regexp"
	"strings"
	"unicode"
)

// FormatTargetID returns an agent-facing bullseye id with the 🎯 prefix
// (🎯T326). Empty / whitespace-only input yields "". Already-prefixed ids
// are not double-wrapped. Matching CLI habit: never bare "T305" in
// system-prompt / brief / nudge inject text.
func FormatTargetID(raw string) string {
	id := NormalizeTargetID(raw)
	if id == "" {
		return ""
	}
	return "🎯" + id
}

// NormalizeTargetID strips optional 🎯 + surrounding space and uppercases
// the leading T (T10.2 form). Empty when input is not a target-shaped id.
func NormalizeTargetID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "🎯")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Accept t10 / T10 / T10.2
	runes := []rune(s)
	if unicode.ToUpper(runes[0]) != 'T' {
		return s // leave non-T strings as trimmed body (caller may still want them)
	}
	runes[0] = 'T'
	return string(runes)
}

// bareTargetIDRe matches bullseye-style T-numbers that lack a 🎯 prefix.
// Used only for inject-path oracles (🎯T326) — not for chat UI parsing.
var bareTargetIDRe = regexp.MustCompile(`(?m)(?:^|[^🎯])\bT\d+(?:\.\d+)*\b`)

// HasBareTargetID reports whether s contains a target id without 🎯.
// Worker names (jv-t27.2-config) use lowercase t and do not match.
func HasBareTargetID(s string) bool {
	return bareTargetIDRe.MatchString(s)
}

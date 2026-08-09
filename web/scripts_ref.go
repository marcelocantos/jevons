// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package web

import "regexp"

// indexScriptSrcRE matches <script src="…"> (single or double quotes).
var indexScriptSrcRE = regexp.MustCompile(`(?i)<script[^>]+src=["']([^"']+)["']`)

// IndexScriptRefs returns the local scripts/… modules an index.html loads,
// in document order, ignoring CDN/absolute URLs and refusing traversal.
//
// The compile-time embed ratchet (🎯T292) checks these against the embedded
// FS; the serving path checks them against whatever tree it is about to
// serve (🎯T374) — the daily daemon runs dev-mode serve-from-disk, so a
// module can be referenced by index.html and absent from the served tree
// long before anyone rebuilds the binary.
func IndexScriptRefs(html []byte) []string {
	var refs []string
	for _, m := range indexScriptSrcRE.FindAllSubmatch(html, -1) {
		src := string(m[1])
		if len(src) < len("scripts/") || src[:len("scripts/")] != "scripts/" {
			continue // CDN / external
		}
		if containsDotDot(src) {
			continue
		}
		refs = append(refs, src)
	}
	return refs
}

func containsDotDot(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '.' && s[i+1] == '.' {
			return true
		}
	}
	return false
}

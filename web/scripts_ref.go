// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package web

import (
	"bytes"
	"regexp"
)

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
//
// Commented-out tags are not loads. That distinction is load-bearing rather
// than pedantic: the serving path turns every ref it cannot find on disk into
// an owner-visible "cockpit assets incomplete" banner, so a <script src> that
// appears only inside an HTML comment — a disabled module, or a wiring
// comment quoting the tag shape it is explaining — would put a false alarm on
// the live cockpit and train the owner to ignore a real one.
func IndexScriptRefs(html []byte) []string {
	var refs []string
	for _, m := range indexScriptSrcRE.FindAllSubmatch(stripHTMLComments(html), -1) {
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

// stripHTMLComments blanks every <!-- … --> span, preserving length so the
// result stays byte-aligned with the input for any caller that reports
// offsets. An unterminated comment swallows the rest of the document, which
// is what a browser does with it too.
func stripHTMLComments(html []byte) []byte {
	openTag := []byte("<!--")
	closeTag := []byte("-->")
	out := append([]byte(nil), html...)
	for i := 0; i+len(openTag) <= len(out); {
		j := bytes.Index(out[i:], openTag)
		if j < 0 {
			break
		}
		start := i + j
		end := len(out)
		if k := bytes.Index(out[start+len(openTag):], closeTag); k >= 0 {
			end = start + len(openTag) + k + len(closeTag)
		}
		for p := start; p < end; p++ {
			if out[p] != '\n' {
				out[p] = ' '
			}
		}
		i = end
	}
	return out
}

func containsDotDot(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '.' && s[i+1] == '.' {
			return true
		}
	}
	return false
}

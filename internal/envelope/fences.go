// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "strings"

// FenceStarts returns the byte offset of every ```jevons fence opener in
// text that begins its own line and carries nothing else on that line.
//
// 🎯T736: a prose mention of the fence is not one. The stored scout-report
// that broke the 🎯T721 handoff on the live send path ended with the
// sentence "… a ```jevons ` fence, e.g. T582's […] shape", and a LastIndex
// over the raw string found that sentence instead of the envelope 5KB above
// it — so the reader parsed prose, failed, and the daemon refused a handoff
// its own refusal text described as exempt. Both filters earn their keep:
// the mention sits mid-line, and its line carries prose after the info
// string (which splitFence alone tolerates, since it only reads field one).
func FenceStarts(text string) []int {
	var out []int
	for off := 0; off < len(text); {
		line, next := text[off:], len(text)
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line, next = line[:nl], off+nl+1
		}
		trimmed := strings.TrimLeft(line, " \t")
		if rest, ok := strings.CutPrefix(trimmed, "```"); ok &&
			strings.EqualFold(strings.TrimSpace(rest), FenceInfo) {
			out = append(out, off+len(line)-len(trimmed))
		}
		off = next
	}
	return out
}

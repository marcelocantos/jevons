// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// 🎯T829: a worker is reaped only on a completion claim it made about ITS OWN
// work, never on one it relays from someone else's message.
//
// The specimen is cl-t119-broker-gate's 2026-09-21T21:10:03Z removal. Its
// report_span was "The owner already ran it at 36a37e0 as GREEN (881ba165),
// and T126 is achieved on it." — a sentence relaying what the OWNER already
// ran on a DIFFERENT target (🎯T126), written mid-work while the seat's own
// gate ("its make gate") was still on the last step and later died with no
// result. 🎯T750 already masks a marker the worker QUOTES (fenced, blockquoted,
// double-quoted, or paired with a citation lead-in on the same line); it does
// not mask a marker the worker RELAYS in its own prose from another party's
// report — "the owner already ran it" carries none of T750's literal quoting
// shapes, so the unmasked scan still reads "achieved" as this seat's own
// claim.
//
// attributionLeadIns mark a SENTENCE as reporting someone ELSE's action
// (owner / parent / PO / supervisor) rather than the worker's own. The whole
// sentence goes, mirroring maskQuotedAndCitedLines' whole-line treatment of a
// citation lead-in — the marker follows the lead-in within that sentence in
// every observed shape.
var attributionLeadIns = []string{
	"the owner already",
	"the owner ran",
	"the owner has already",
	"the owner reported",
	"the owner said",
	"the owner confirmed",
	"owner already ran",
	"the parent already",
	"the parent ran",
	"the parent reported",
	"the parent said",
	"the po already",
	"the po ran",
	"the po reported",
	"the po said",
	"the supervisor already",
	"the supervisor reported",
	"already ran it",
	"per the owner",
	"per the parent",
	"as the owner",
	"the owner's message",
	"the owner's words",
}

// maskAttributedClauses blanks any SENTENCE (delimited by '.', '!', '?', or a
// newline) that carries an attribution lead-in — reported speech about
// someone else's action — regardless of whether it also names a target that
// happens to be the worker's own. Composed into claimScanText (🎯T750) so
// every reap-decision caller (hasCompletionClaim, FindCompletionClaim,
// CitedCompletionClaim) reads the same masked text.
func maskAttributedClauses(b []byte) {
	for start := 0; start < len(b); {
		end := start
		for end < len(b) && b[end] != '.' && b[end] != '!' && b[end] != '?' && b[end] != '\n' {
			end++
		}
		// Include the delimiter itself in the scanned sentence but not in
		// what gets blanked past it (blank() already stops at end).
		limit := end
		if limit < len(b) {
			limit++
		}
		sentence := string(b[start:limit])
		for _, lead := range attributionLeadIns {
			if strings.Contains(sentence, lead) {
				blank(b, start, limit)
				break
			}
		}
		start = end + 1
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// 🎯T750: a worker is reaped only on a completion claim it MADE, never on one
// it QUOTED.
//
// Every completion classifier in this package scans the whole report for a
// completion word and takes the first hit. That reads a marker the worker is
// citing — its own objective block, a code span, a line it is explicitly
// declining to emit — as the worker's own claim, and a reap is irreversible.
//
// The jv-t745-busy-pane checkpoint of 2026-09-20T17:51:01Z is the shape: four
// numbered "Still to do" items, a "Residual:" paragraph, and a closing "So
// this objective is not yet complete, and I'm not emitting a GOAL_STATUS
// line." Its ONLY completion marker is the negated one in that last sentence,
// yet hasCompletionClaim matched it, hasAcceptedRisk matched "Residual:", and
// hasFinishShape therefore returned a finish. What kept that seat alive was
// 🎯T716's checkpoint ask-class veto reading the word "Checkpoint." — delete
// that one word and the same text reaps a worker that has just said, in
// terms, that it has not finished. The claim side must not depend on an ask
// list catching the phrasing; see the 🎯T445 note in finish_shape.go for why
// the burden of proof belongs on the finish.
//
// (The reap the row was filed from is a separate matter: report_offset=3910
// names the 17:56:27 report, whose final line is a bare, unquoted
// "GOAL_STATUS: complete". That reap was correct on that text. The defect is
// real; the specimen reap was not an instance of it.)
//
// claimScanText blanks the regions where a marker is cited rather than
// asserted, byte-for-byte so every offset a caller reports still indexes the
// original report. The veto is one-way, like the rest of the reap chain: a
// real finish that phrases itself inside a masked region costs an idle
// process the sentinel already prunes, and nothing worse.
func claimScanText(lower string) string {
	b := []byte(lower)
	maskFencedCode(b)
	maskInlineCode(b)
	maskQuotedAndCitedLines(b)
	maskNegatedClauses(b)
	return string(b)
}

func blank(b []byte, start, end int) {
	for i := start; i < end && i < len(b); i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

// maskFencedCode blanks ``` fenced blocks, fences included. An unterminated
// fence masks to the end: a report that opens a block and never closes it is
// quoting everything after it.
func maskFencedCode(b []byte) {
	s := string(b)
	for i := 0; ; {
		open := strings.Index(s[i:], "```")
		if open < 0 {
			return
		}
		open += i
		close := strings.Index(s[open+3:], "```")
		end := len(b)
		if close >= 0 {
			end = open + 3 + close + 3
		}
		blank(b, open, end)
		if close < 0 {
			return
		}
		i = end
	}
}

// maskInlineCode blanks `…` spans that open and close on one line. A lone
// backtick is punctuation, not a span, and is left alone.
func maskInlineCode(b []byte) {
	for i := 0; i < len(b); i++ {
		if b[i] != '`' {
			continue
		}
		j := i + 1
		for j < len(b) && b[j] != '`' && b[j] != '\n' {
			j++
		}
		if j < len(b) && b[j] == '`' {
			blank(b, i, j+1)
			i = j
		}
	}
}

// citationLeadIns mark a line as being ABOUT the marker rather than emitting
// it: the objective block a harness pastes in, and the sentence a worker
// writes to say it is withholding the marker. The whole line goes, because in
// both shapes the marker follows the lead-in on that same line.
var citationLeadIns = []string{
	"emit exactly",
	"emit the marker",
	"emit a goal_status",
	"emit the goal_status",
	"emitting the marker",
	"emitting a goal_status",
	"emitting the goal_status",
	"emitting a completion",
	"not emitting",
	"do not emit",
	"don't emit",
	"without emitting",
	"instead of emitting",
	"emit neither",
	"emit nothing",
	"goal_status: blocked",
}

// maskQuotedAndCitedLines blanks markdown blockquote lines, lines carrying a
// citation lead-in, and double-quoted spans within a line.
func maskQuotedAndCitedLines(b []byte) {
	for start := 0; start < len(b); {
		end := start
		for end < len(b) && b[end] != '\n' {
			end++
		}
		line := string(b[start:end])
		trimmed := strings.TrimLeft(line, " \t>")
		switch {
		case len(line) != len(trimmed) && strings.Contains(line, ">"):
			// Blockquote: the worker is reproducing someone else's text.
			blank(b, start, end)
		default:
			cited := false
			for _, lead := range citationLeadIns {
				if strings.Contains(line, lead) {
					cited = true
					break
				}
			}
			if cited {
				blank(b, start, end)
			} else {
				maskDoubleQuoted(b, start, end)
			}
		}
		start = end + 1
	}
}

// maskDoubleQuoted blanks "…" and “…” spans that open and close inside one
// line. An unmatched quote is an apostrophe or a stray, and is left alone.
func maskDoubleQuoted(b []byte, start, end int) {
	for i := start; i < end; i++ {
		if b[i] != '"' {
			continue
		}
		j := i + 1
		for j < end && b[j] != '"' {
			j++
		}
		if j < end {
			blank(b, i, j+1)
			i = j
		}
	}
}

// claimNegatorWords negate a completion claim standing in the same clause.
// Contraction stems ("isn", "didn") are listed bare because the apostrophe
// splits the word.
var claimNegatorWords = map[string]bool{
	"not": true, "never": true, "cannot": true, "no": true, "nor": true,
	"without": true, "isn": true, "wasn": true, "aren": true, "weren": true,
	"didn": true, "doesn": true, "don": true, "won": true, "haven": true,
	"hasn": true, "hadn": true, "couldn": true, "shouldn": true,
	"wouldn": true, "ain": true, "yet": true,
}

// maskNegatedClauses blanks any clause that carries both a completion marker
// and a word that negates it — "the objective is not yet complete", "nothing
// is finished". Clause boundaries are finish_shape.go's, so the unit is the
// same one 🎯T445 uses to decide a bare claim.
func maskNegatedClauses(b []byte) {
	start := 0
	for i := 0; i <= len(b); i++ {
		if i < len(b) && !finishClauseDelimiter(rune(b[i])) {
			continue
		}
		if clauseNegatesClaim(string(b[start:i])) {
			blank(b, start, i)
		}
		start = i + 1
	}
}

func clauseNegatesClaim(clause string) bool {
	if !hasAnyCompletionMarker(clause) {
		return false
	}
	for _, w := range strings.FieldsFunc(clause, func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	}) {
		if claimNegatorWords[w] {
			return true
		}
	}
	return false
}

func hasAnyCompletionMarker(lower string) bool {
	for _, m := range completionClaimMarkers {
		if indexWordish(lower, m) >= 0 {
			return true
		}
	}
	return false
}

// CitedCompletionClaim names the completion marker a report CITES without
// asserting — the marker claimScanText masked — together with the sentence it
// sits in.
//
// 🎯T750, second half: the save has to be visible. A report kept because its
// only completion marker was quoted takes the silent `return` in
// maybeReapDoneWorkAgent's !ok branch, so it leaves no lifecycle record at
// all — while 🎯T395, 🎯T470 and 🎯T581 each log theirs. That absence is
// indistinguishable from a sink that never ran (🎯T426 / 🎯T744 dark stream),
// which is exactly how the first live provocation of this fix came back
// unreadable. ok is true only when the report would have read as a finish
// BEFORE the mask and does not after it, so the record marks the save this
// target makes and not every passing mention of a completion word.
func CitedCompletionClaim(report string) (marker, span string, offset int, ok bool) {
	lower := asciiLower(report)
	if hasCompletionClaim(lower) {
		return "", "", 0, false // asserted, not cited
	}
	if !wouldHaveFinishedBeforeT750(lower) {
		return "", "", 0, false
	}
	best, at := "", -1
	for _, m := range completionClaimMarkers {
		i := indexWordish(lower, m)
		if i < 0 || (at >= 0 && i >= at) {
			continue
		}
		best, at = m, i
	}
	if at < 0 {
		return "", "", 0, false
	}
	span, offset = matchedSentence(report, at, at+len(best))
	return best, span, offset, true
}

// wouldHaveFinishedBeforeT750 is hasFinishShape as it read before the mask:
// a completion word anywhere, plus accepted-risk, oracle evidence, or a bare
// claim clause. Kept here rather than left implicit so the log records a save
// only where there was something to save from.
func wouldHaveFinishedBeforeT750(lower string) bool {
	if !hasAnyCompletionMarker(lower) {
		return false
	}
	if hasAcceptedRisk(lower) || hasOracleEvidence(lower) {
		return true
	}
	for _, clause := range strings.FieldsFunc(lower, finishClauseDelimiter) {
		if bareClaimClause(clause) {
			return true
		}
	}
	return false
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// 🎯T581: a completion word used as a LOCAL clause is not a finish.
//
// jv-t564-no-ctx-ceiling was reaped finished_work on "Status: reading done,
// no tool hung; implementing now — config default flip … Now docs …". The
// "done" there closes a sub-step ("reading done", "recon done"); the same
// sentence goes straight on to present-progressive next-work language. T577's
// forward-looking list ("Next step", "I'll resume") never fires on that
// grammar, and growing the list one incident at a time is the anti-pattern
// this file avoids: the rule is the SHAPE — a done-word whose sentence (or the
// one after it) continues with "<verb>ing now/next" or a clause opening on
// "now …" / "next …" — not a phrase.
//
// The check is applied per claim: only when EVERY completion word in the
// report is local does the report lose its finish shape. A bare "Done." next
// to a local "reading done" still reaps — the bare clause is the worker's own
// voice (🎯T445 residual). Ambiguity resolves toward keeping the seat.

// nextWorkAdverbs follow a present participle ("implementing now") or open a
// next-work clause ("now docs", "next: wire the governor").
var nextWorkAdverbs = map[string]bool{"now": true, "next": true}

// notProgressiveING are common -ing words that are not verbs in progress.
var notProgressiveING = map[string]bool{
	"thing": true, "nothing": true, "something": true, "anything": true,
	"everything": true, "during": true, "string": true, "bring": true,
	"morning": true, "evening": true, "warning": true, "setting": true,
	"settings": true, "ring": true, "king": true, "wing": true,
}

// allCompletionClaimsLocal is true when lower carries at least one completion
// claim and every one of them is a local clause followed by next-work language.
func allCompletionClaimsLocal(lower string) bool {
	sentences := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '.' || r == '!' || r == '?' || r == '\n'
	})
	claims := 0
	for i, sent := range sentences {
		if !hasCompletionClaim(sent) {
			continue
		}
		claims++
		clauses := strings.FieldsFunc(sent, finishClauseDelimiter)
		if i+1 < len(sentences) {
			clauses = append(clauses, strings.FieldsFunc(sentences[i+1], finishClauseDelimiter)...)
		}
		// The claim's own clause must precede the next-work clause.
		claimAt := -1
		for j, c := range clauses {
			if hasCompletionClaim(c) {
				claimAt = j
				break
			}
		}
		local := false
		for _, c := range clauses[claimAt+1:] {
			if nextWorkClause(c) {
				local = true
				break
			}
		}
		if !local {
			return false
		}
	}
	return claims > 0
}

// nextWorkClause is true when clause reads as work in progress or about to
// start: "<verb>ing now", "<verb>ing next", "now <more>", "next <more>".
func nextWorkClause(clause string) bool {
	words := strings.Fields(clause)
	if len(words) == 0 {
		return false
	}
	// "next: the writer" splits on the colon, leaving the label alone.
	if nextWorkAdverbs[strings.Trim(words[0], "*_`~\"'")] {
		return true
	}
	for i := 1; i < len(words); i++ {
		if nextWorkAdverbs[strings.Trim(words[i], "*_`~\"'")] && presentParticiple(words[i-1]) {
			return true
		}
	}
	return false
}

func presentParticiple(w string) bool {
	w = strings.Trim(w, "*_`~\"'")
	return len(w) >= 5 && strings.HasSuffix(w, "ing") && !notProgressiveING[w]
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// 🎯T445: a finish is recognised by its own shape, not by the absence of a
// recognised ask.
//
// The reap chain used to read "completion word anywhere, and no ask-class
// marker fired" as a finish. Every ask class (🎯T395 / 🎯T439 / 🎯T497) is a
// phrase list, so an ask worded outside the lists fell through to the
// DESTRUCTIVE branch: the worker was reaped mid-question, its asking context
// destroyed — jv-t439-reap-request named this exact residual in its own finish
// report, and this target's first seat was then falsely reaped on an explicit
// checkpoint, proving the point. The two misclassifications are not symmetric:
// a finish read as an ask costs an idle process the sentinel already prunes; an
// ask read as a finish is irreversible. So the default must fall the safe way
// for every phrasing nobody thought of, which means the finish side has to
// carry the burden of proof.
//
// A positive finish shape is one of:
//   - a BARE CLAIM CLAUSE — a clause that IS the claim ("Done.", "Mission
//     complete.", "All finished"), with nothing in it but completion vocabulary
//     and glue words. This is the 🎯T195 imperfect report, which must go on
//     reaping: a clause whose entire content is the claim hands nothing back.
//     A claim buried in a content-bearing clause ("Config migration done. One
//     thing I need from you: …") no longer counts — the surrounding content is
//     exactly where an unlisted ask lives;
//   - a claim plus ORACLE EVIDENCE (SHA / named tests / green) — the 🎯T31
//     finish report;
//   - a claim plus ACCEPTED-RISK language — the 🎯T31.1 residual path.
//
// The ask classes still veto after this (a report can carry a SHA and still
// end on a question); this narrows what can reach the reap at all. Residual: a
// report that pairs a bare "Done." with an unlisted ask elsewhere still reaps —
// the bare clause is a positive claim in the worker's own voice.

// finishGlueWords may accompany a completion claim in a clause without making
// it content-bearing. Deliberately tiny: every word admitted here widens what
// reaps, and the bias runs the other way.
var finishGlueWords = map[string]bool{
	"all": true, "and": true, "is": true, "are": true, "was": true,
	"were": true, "now": true, "the": true, "a": true, "an": true,
	"work": true, "mission": true, "target": true, "task": true,
	"everything": true, "fully": true, "successfully": true,
	"officially": true, "yes": true, "ok": true, "okay": true,
}

// finishClauseDelimiter splits a report into clauses for the bare-claim test.
// Commas count: "All finished, ready for review" is two claims, not one clause
// with content.
func finishClauseDelimiter(r rune) bool {
	switch r {
	case '.', ',', ';', ':', '!', '?', '\n', '(', ')', '—', '–':
		return true
	}
	return false
}

// hasFinishShape reports whether lower — a trimmed, lowercased report —
// positively claims completion in one of the recognised finish shapes (🎯T445).
func hasFinishShape(lower string) bool {
	if !hasCompletionClaim(lower) {
		return false
	}
	if hasAcceptedRisk(lower) || hasOracleEvidence(lower) {
		return true
	}
	return hasBareClaimClause(lower)
}

// hasBareClaimClause is true when some clause of lower consists of a completion
// claim and glue words alone — the clause IS the claim (🎯T195's "Done.",
// "Mission complete."), rather than a sentence that happens to contain a
// completion word.
func hasBareClaimClause(lower string) bool {
	for _, clause := range strings.FieldsFunc(lower, finishClauseDelimiter) {
		if bareClaimClause(clause) {
			return true
		}
	}
	return false
}

func bareClaimClause(clause string) bool {
	claimed := false
	for _, m := range completionClaimMarkers {
		for {
			i := indexWordish(clause, m)
			if i < 0 {
				break
			}
			claimed = true
			clause = clause[:i] + " " + clause[i+len(m):]
		}
	}
	if !claimed {
		return false
	}
	for _, w := range strings.FieldsFunc(clause, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if allDigits(w) {
			continue // "100%" and version-ish tokens are decoration, not content
		}
		if !finishGlueWords[w] {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

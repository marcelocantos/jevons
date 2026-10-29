// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package relayroute

import "strings"

// disclaimed reports whether the body explicitly disclaims completion
// (🎯T860): "not claiming done/complete" and "reporting status only" both
// supply the raw substrings "done"/"complete" that a naive done-word scan
// reads as a claim, so the negation must be checked before those words are
// trusted.
func disclaimed(s string) bool {
	s = strings.ToLower(s)
	for _, p := range []string{
		"not claiming done",
		"not claiming complete",
		"no assumed completion",
		"reporting status only",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// externallyAttributedOracle reports whether the body's oracle-shaped
// markers (GATE/GREEN, "oracle", SHA) are quoted as evidence belonging to a
// named OTHER agent or a sibling's commit, rather than the reporter's own
// (🎯T860). A reporter quoting someone else's green does not thereby earn
// oracle_done for its own report.
func externallyAttributedOracle(s string) bool {
	s = strings.ToLower(s)
	for _, p := range []string{
		"another agent",
		"sibling agent",
		"sibling's commit",
		"'s commit",
		"'s evidence",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// oracleDone is the prose keyword classifier for oracle-backed completion.
// 🎯T766.4: Classify and Reason no longer call this for unenveloped reports;
// a declared finish-report envelope is the routing source of truth. The
// function remains so disclaimer and attribution guards stay pinned when
// prose is scanned (tests, residual paths, and the 2026-09-23 specimen).
func oracleDone(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if disclaimed(s) {
		return false
	}
	done := strings.Contains(s, "done") || strings.Contains(s, "achieved") ||
		strings.Contains(s, "complete") || strings.Contains(s, "finished")
	if !done {
		return false
	}
	if externallyAttributedOracle(s) {
		return false
	}
	oracle := strings.Contains(s, "gate") && strings.Contains(s, "green")
	oracle = oracle || strings.Contains(s, "oracle")
	oracle = oracle || (strings.Contains(s, "sha") && (strings.Contains(s, "test") || strings.Contains(s, "green")))
	return oracle
}

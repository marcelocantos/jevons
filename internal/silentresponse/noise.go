// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package silentresponse

import "strings"

// 🎯T1054: server-side classification of ROUTINE worker finish/progress
// restatements, so the overseer does not have to remember to type [silent]
// on every "Good, X" / "Confirmed, consistent" line. This is deliberately
// narrow: it recognises the shape of a routine no-action restatement, and it
// refuses to fire whenever the text carries any signal that might matter —
// a question, an anomaly/incident word, a decision point, or any content
// past a short lead-in. False-negative (stays visible) is the safe failure
// here; false-positive (a material message silently dropped) is not.

// routineLeadIns are the opening phrases a pure restatement starts with.
// Matched case-insensitively against the trimmed text's first line only —
// never against the body, so a report that happens to use one of these
// words mid-paragraph is untouched.
var routineLeadIns = []string{
	"good,",
	"confirmed,",
	"noted,",
	"acknowledged,",
	"received, ",
	"understood,",
	"ok, ",
	"okay, ",
	"got it,",
}

// escapeWords veto auto-suppression regardless of lead-in: any of these
// appearing anywhere in the text means a human reads it. This is the
// hypothetical-safety-incident-phrase regression case (🎯T1054 acceptance 2)
// and the real-incident case (acceptance 2): both must pass through.
var escapeWords = []string{
	"safety", "incident", "unsafe", "danger", "dangerous", "harm",
	"breach", "security", "leak", "exploit", "credential", "secret",
	"?", // any question mark anywhere means someone is asking something
	"please", "decision", "approve", "authoriz", "block", "blocked",
	"fail", "error", "crash", "panic", "down", "outage", "data loss",
	"urgent", "immediately",
}

// IsRoutineNoise reports whether text is a routine, no-action restatement of
// a worker report — the kind of line 🎯T1053 says should get no response at
// all. It is intentionally conservative: it only fires on a short message
// whose first line is a bare acknowledgement lead-in and whose whole body
// carries none of the escape words. Longer or hedged text, anything with a
// question, and anything naming risk/incident/decision content is left
// Visible.
func IsRoutineNoise(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	for _, w := range escapeWords {
		if strings.Contains(lower, w) {
			return false
		}
	}
	firstLine := t
	if i := strings.IndexByte(firstLine, '\n'); i >= 0 {
		firstLine = firstLine[:i]
	}
	firstLineLower := strings.ToLower(strings.TrimSpace(firstLine))
	matched := false
	for _, lead := range routineLeadIns {
		if strings.HasPrefix(firstLineLower, lead) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	// Cap: a routine restatement is short. Anything long enough to carry a
	// decision or multi-point status is left visible rather than risk
	// burying content behind a lead-in match.
	const maxRoutineRunes = 240
	if len([]rune(t)) > maxRoutineRunes {
		return false
	}
	return true
}

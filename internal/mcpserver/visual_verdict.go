// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// VisualVerdictClass is a hermetic classification of a finish report for
// the cockpit visual-sanity pass (🎯T493.1). Heuristic only — not a pixel
// judge; overseer judgment still applies.
type VisualVerdictClass int

const (
	// VisualVerdictNotApplicable: not visual cockpit work, or no done claim.
	VisualVerdictNotApplicable VisualVerdictClass = iota
	// VisualVerdictPresent: four-part prose look; not a contradiction.
	VisualVerdictPresent
	// VisualVerdictMissing: cockpit work claimed done without the prose look.
	VisualVerdictMissing
	// VisualVerdictYesAgainstNo: says yes while describing an automatic no.
	VisualVerdictYesAgainstNo
	// VisualVerdictFalseGreen: prose says no but a green journey is treated as enough.
	VisualVerdictFalseGreen
)

func (c VisualVerdictClass) String() string {
	switch c {
	case VisualVerdictNotApplicable:
		return "not_applicable"
	case VisualVerdictPresent:
		return "present"
	case VisualVerdictMissing:
		return "missing"
	case VisualVerdictYesAgainstNo:
		return "yes_against_no"
	case VisualVerdictFalseGreen:
		return "false_green"
	default:
		return "unknown"
	}
}

// visualCockpitMarkers are phrases that mean the report is about what the
// owner sees in #messages. Identifier-shaped census fields count: those
// are exactly the greens that were substituted for a look.
var visualCockpitMarkers = []string{
	"#messages",
	"visibleinscroller",
	"visiblebubbles",
	"viewport census",
	"messages viewport",
	"pintoliveend",
	"pin-to-bottom",
	"pin to live end",
	"pin to bottom",
	"virtualizemessages",
	"turn-slot",
	"turn slot",
	"empty slot",
	"emptyslots",
	"latest fab",
	"latest button",
	"cockpit screenshot",
	"chat pane",
	"hard-reload pane",
	"hard reload pane",
	"viewport screenshot",
	"normal chat transcript",
	"normal transcript after",
}

var visualInkWords = []string{"ink", "bubble", "bubbles", "turn", "turns"}

var visualEmptyWords = []string{
	"empty", "desert", "void", "packed", "blank", "whitespace",
}

var falseGreenJourneyMarkers = []string{
	"j19",
	"test-journey",
	"journey green",
	"journey passed",
	"journey pass",
}

var oracleFixMarkers = []string{
	"false green",
	"false-green",
	"fix the oracle",
	"fixing the oracle",
	"tighten the journey",
	"tightening the journey",
}

// LooksLikeVisualCockpitWork reports whether the text is about owner-visible
// #messages / pin / virtualize / replay / fold / slot / spacing work.
func LooksLikeVisualCockpitWork(report string) bool {
	s := strings.ToLower(strings.TrimSpace(report))
	if s == "" {
		return false
	}
	for _, m := range visualCockpitMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// HasVisualProseVerdict is true when the report answers the four required
// look questions in prose: ink on screen, empty pane, Latest, and yes/no
// to a normal chat transcript after a hard reload.
func HasVisualProseVerdict(report string) bool {
	s := strings.ToLower(report)
	if !hasVisualInkCall(s) {
		return false
	}
	if !hasVisualEmptyCall(s) {
		return false
	}
	if !containsWordish(s, "latest") {
		return false
	}
	ans := normalTranscriptAnswer(s)
	return ans == "yes" || ans == "no"
}

// LooksLikeMissingVisualVerdict is the 🎯T493.1 anti-pattern: visual cockpit
// work claimed done, with a green metric or caption standing in for a look.
func LooksLikeMissingVisualVerdict(report string) bool {
	return ClassifyVisualVerdict(report) == VisualVerdictMissing
}

// LooksLikeAutomaticVisualNo is true when the report describes one of the
// automatic-no pictures: leftover bubble, Latest showing, or a desert pane.
func LooksLikeAutomaticVisualNo(report string) bool {
	return looksLikeAutomaticVisualNo(strings.ToLower(report))
}

// LooksLikeYesAgainstAutomaticNo is a yes to "normal transcript" while the
// same report describes an automatic-no picture.
func LooksLikeYesAgainstAutomaticNo(report string) bool {
	return ClassifyVisualVerdict(report) == VisualVerdictYesAgainstNo
}

// LooksLikeFalseGreenVisualJourney is a prose no plus a green journey
// treated as enough to finish — the journey is the false green.
func LooksLikeFalseGreenVisualJourney(report string) bool {
	return ClassifyVisualVerdict(report) == VisualVerdictFalseGreen
}

// ClassifyVisualVerdict classifies a finish report for 🎯T493.1.
// Priority: not-applicable > missing > yes-against-no > false-green > present.
func ClassifyVisualVerdict(report string) VisualVerdictClass {
	if !LooksLikeVisualCockpitWork(report) {
		return VisualVerdictNotApplicable
	}
	lower := strings.ToLower(report)
	if !hasCompletionClaim(lower) {
		return VisualVerdictNotApplicable
	}
	if !HasVisualProseVerdict(report) {
		return VisualVerdictMissing
	}
	ans := normalTranscriptAnswer(lower)
	autoNo := looksLikeAutomaticVisualNo(lower)
	if autoNo && ans == "yes" {
		return VisualVerdictYesAgainstNo
	}
	if ans == "no" && citesGreenJourney(lower) && !mentionsOracleFix(lower) {
		return VisualVerdictFalseGreen
	}
	return VisualVerdictPresent
}

func hasVisualInkCall(lower string) bool {
	for _, w := range visualInkWords {
		if containsWordish(lower, w) {
			return true
		}
	}
	return false
}

func hasVisualEmptyCall(lower string) bool {
	if strings.Contains(lower, "white space") {
		return true
	}
	if strings.Contains(lower, "empty canvas") {
		return true
	}
	for _, w := range visualEmptyWords {
		if containsWordish(lower, w) {
			return true
		}
	}
	return false
}

func looksLikeAutomaticVisualNo(lower string) bool {
	if containsWordish(lower, "leftover") {
		return true
	}
	if containsWordish(lower, "desert") {
		return true
	}
	if strings.Contains(lower, "more empty") {
		return true
	}
	if latestShowing(lower) {
		return true
	}
	return false
}

func latestShowing(lower string) bool {
	if !containsWordish(lower, "latest") {
		return false
	}
	if containsWordish(lower, "hidden") || containsWordish(lower, "absent") {
		return false
	}
	return containsWordish(lower, "showing") ||
		containsWordish(lower, "visible") ||
		containsWordish(lower, "present") ||
		strings.Contains(lower, "latest on a hard reload")
}

func citesGreenJourney(lower string) bool {
	if !strings.Contains(lower, "green") &&
		!strings.Contains(lower, "pass") &&
		!strings.Contains(lower, "passed") {
		return false
	}
	for _, m := range falseGreenJourneyMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func mentionsOracleFix(lower string) bool {
	for _, m := range oracleFixMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// normalTranscriptAnswer returns "yes", "no", or "" for the required
// "does this look like a normal chat transcript" sentence.
func normalTranscriptAnswer(lower string) string {
	if !strings.Contains(lower, "normal") {
		return ""
	}
	if !strings.Contains(lower, "transcript") && !strings.Contains(lower, "chat") {
		return ""
	}
	if strings.Contains(lower, "does not look like a normal") ||
		strings.Contains(lower, "doesn't look like a normal") ||
		strings.Contains(lower, "not a normal chat transcript") ||
		strings.Contains(lower, "not a normal transcript") {
		return "no"
	}
	idx := strings.Index(lower, "normal")
	if idx < 0 {
		return ""
	}
	tail := lower[idx:]
	// The load-bearing form is "... after a hard reload? Yes." — matchedSentence
	// splits on '?', so the answer is the first yes/no after the question mark.
	if q := strings.Index(tail, "?"); q >= 0 {
		after := strings.TrimSpace(tail[q+1:])
		if ans := leadingYesNo(after); ans != "" {
			return ans
		}
	}
	yi := lastWordish(tail, "yes")
	ni := lastWordish(tail, "no")
	switch {
	case yi < 0 && ni < 0:
		return ""
	case yi > ni:
		return "yes"
	default:
		return "no"
	}
}

func leadingYesNo(s string) string {
	if strings.HasPrefix(s, "yes") && !wordRuneAt(s, 3) {
		return "yes"
	}
	if strings.HasPrefix(s, "no") && !wordRuneAt(s, 2) {
		return "no"
	}
	return ""
}

func lastWordish(s, phrase string) int {
	last := -1
	for i := 0; i+len(phrase) <= len(s); {
		j := indexWordish(s[i:], phrase)
		if j < 0 {
			return last
		}
		last = i + j
		i = last + len(phrase)
	}
	return last
}

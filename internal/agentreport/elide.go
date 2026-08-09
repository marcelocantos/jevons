// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package agentreport makes a fleet agent's turn report survive the trip to
// the overseer: it is stored durably before it is delivered, and if it cannot
// be delivered whole the cut is explicit and carries a retrieval handle.
//
// 🎯T388. Three reports were silently cut in one session, and every time the
// lost tail was the part that mattered:
//
//   - claudia-po cut at "MatchTur…", losing the two items it had explicitly
//     flagged as needing the overseer.
//   - jv-t372-auto cut at "The inventory doc (§2.3 F-HARNESS-1) tel…", losing
//     a correction it had flagged as changing a ruling already in flight to
//     the owner.
//   - og-t21-stranded-pad cut mid-sentence in its first report.
//
// The old cut was `text[:1997] + "..."` on the notify path
// (internal/mcpserver/agents.go). Two things made it lossy rather than merely
// lumpy. First, the marker was three dots — indistinguishable from an author's
// own ellipsis, so the reader had no way to know anything was missing. Second,
// it kept the HEAD, and reports are written with conclusions and asks at the
// END; a head-only cut eats exactly the region worth sending.
//
// So elision here keeps BOTH ends and elides the middle, with an
// unmistakable marker between them naming the call that returns the whole
// thing. A report that fits is returned byte-identical, with no marker and no
// handle — a short report must not acquire chrome it does not need.
package agentreport

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// DeliveryBound is the byte bound on report text delivered to the overseer.
// It is the pre-🎯T388 product bound, kept deliberately: the target is that a
// cut be visible and recoverable, not that the bound be raised (raising it
// only moves the same silent failure to a larger report).
const DeliveryBound = 2000

// tailShare is the fraction of the delivery budget given to the END of the
// report. Conclusions and asks live there, so the tail outranks the head when
// the budget is tight — the opposite of the pre-fix head-only cut.
const tailShare = 0.6

// minSideRunes keeps each retained side legible rather than a token stub.
const minSideRunes = 80

// Elision is the outcome of preparing a report for delivery.
type Elision struct {
	// Text is what goes on the wire. Byte-identical to the input when the
	// report fits within the bound.
	Text string
	// Truncated reports whether anything was elided. False means Text is the
	// whole report and no retrieval handle was added.
	Truncated bool
	// TotalBytes is the length of the original report.
	TotalBytes int
	// ElidedBytes is how much of the middle was removed (0 when not truncated).
	ElidedBytes int
}

// Handle names the durable copy of a report so the elision marker can tell
// the overseer exactly what to call. A zero Handle renders the marker without
// a call, which is still honest — the cut stays visible — but callers on the
// product path should always pass one.
type Handle struct {
	Agent    string
	ReportID string
}

// Empty reports whether the handle names nothing retrievable.
func (h Handle) Empty() bool {
	return strings.TrimSpace(h.Agent) == "" || strings.TrimSpace(h.ReportID) == ""
}

// TruncationMarkerPrefix is the stable, greppable opening of the marker. Both
// the oracle and any consumer that wants to detect an elided delivery match on
// this rather than on the prose around it.
const TruncationMarkerPrefix = "[⚠️ REPORT TRUNCATED"

// Elide prepares text for delivery under bound bytes.
//
// Within the bound, the report is returned untouched: Truncated is false and
// Text == text exactly. Over the bound, the head and the tail are both kept
// and the middle is replaced by an explicit marker naming h.
//
// The marker is always present when Truncated is true, even if it means the
// delivered text exceeds bound — a bound is a sizing heuristic, whereas a
// silent cut is the defect this exists to eliminate. Never trade the marker
// away to satisfy the number.
func Elide(text string, bound int, h Handle) Elision {
	total := len(text)
	if bound <= 0 {
		bound = DeliveryBound
	}
	if total <= bound {
		return Elision{Text: text, TotalBytes: total}
	}

	marker := truncationMarker(0, total, h)
	// The marker's own length depends on the elided count, which depends on
	// the marker's length. One re-render after the first estimate converges:
	// the count only grows the marker by a digit or two, which the min-side
	// floors absorb.
	head, tail := splitSides(text, bound-len(marker))
	elided := total - len(head) - len(tail)
	marker = truncationMarker(elided, total, h)
	head, tail = splitSides(text, bound-len(marker))
	elided = total - len(head) - len(tail)
	marker = truncationMarker(elided, total, h)

	return Elision{
		Text:        head + marker + tail,
		Truncated:   true,
		TotalBytes:  total,
		ElidedBytes: elided,
	}
}

// splitSides returns the leading and trailing runs of text that together fit
// in budget bytes, giving the tail the larger share. Both sides are cut on
// rune boundaries so the wire never carries a mangled character, and each side
// keeps at least minSideRunes' worth of text (or its whole share) so neither
// end degenerates into a stub.
func splitSides(text string, budget int) (head, tail string) {
	if budget < 2*minSideRunes {
		budget = 2 * minSideRunes
	}
	if budget >= len(text) {
		return text, ""
	}
	tailBudget := int(float64(budget) * tailShare)
	headBudget := budget - tailBudget
	head = trimToRuneBoundaryHead(text, headBudget)
	tail = trimToRuneBoundaryTail(text[len(head):], tailBudget)
	return head, tail
}

// trimToRuneBoundaryHead keeps the longest valid-UTF-8 prefix within n bytes,
// preferring to end on a line break so the retained head reads as whole lines.
func trimToRuneBoundaryHead(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n <= 0 {
		return ""
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, '\n'); i > len(cut)/2 {
		cut = cut[:i+1]
	}
	return cut
}

// trimToRuneBoundaryTail keeps the longest valid-UTF-8 suffix within n bytes,
// preferring to start on a line break.
func trimToRuneBoundaryTail(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n <= 0 {
		return ""
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	cut := s[start:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < len(cut)/2 {
		cut = cut[i+1:]
	}
	return cut
}

// truncationMarker renders the visible cut. It states what was removed and
// where the whole thing lives, in a form the overseer can paste back as a
// tool call — recoverability is the point, not just visibility.
func truncationMarker(elided, total int, h Handle) string {
	var b strings.Builder
	b.WriteString("\n\n")
	b.WriteString(TruncationMarkerPrefix)
	fmt.Fprintf(&b, " — %d of %d bytes elided from the MIDDLE of this report.", elided, total)
	if h.Empty() {
		b.WriteString("\nThe head and tail above and below are intact; the middle is not in this message.")
	} else {
		fmt.Fprintf(&b, "\nFull text:  jevons_agent_report_read name=%q report_id=%q", h.Agent, h.ReportID)
		fmt.Fprintf(&b, "\nOne part:   jevons_agent_report_read name=%q report_id=%q section=\"<heading>\"", h.Agent, h.ReportID)
		b.WriteString("\nThis works after the agent deregisters — the report is stored, not held by the process.")
	}
	b.WriteString("]\n\n")
	return b.String()
}

// IsTruncatedDelivery reports whether a delivered message carries the
// explicit marker. Used by tests and by any consumer that wants to know a
// message is partial without re-deriving the bound.
func IsTruncatedDelivery(delivered string) bool {
	return strings.Contains(delivered, TruncationMarkerPrefix)
}

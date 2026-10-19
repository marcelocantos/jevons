// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"unicode/utf8"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/noopwedge"
)

// 🎯T395: a worker that asks the overseer a question is never reaped as
// finished.
//
// The 🎯T165 / 🎯T195 auto-reap keys off completion words appearing anywhere in
// a terminal report. jv-t387-spawn-turn-begins was briefed to establish a
// mechanism and report BEFORE changing anything; it opened its report with
// "Diagnosis complete", laid out four rows of first-hand evidence, and closed
// by asking which side of the jevons/claudia boundary the fix belonged on. The
// word "complete" reaped it. The overseer's answer then bounced off "agent is
// not running" and the diagnosis was stranded.
//
// The bias was backwards. Reaping is the destructive branch: it costs the agent
// its context and its mission, and it is not undoable. Leaving an agent
// registered costs an idle process, and the sentinel already prunes those. So
// ambiguity must resolve toward KEEPING the agent, and a decision request is
// not ambiguity at all — it is positive evidence the mission is still open.
//
// This is a narrowing of the reap, not a disabling of it. A report that claims
// completion and asks for nothing still reaps, including the imperfect bare
// "done" without oracle markers that 🎯T195 deliberately catches.
//
// 🎯T439 is the same lesson one step further out. jv-t435-reap-event was started
// carrying only its target id, correctly refused to write code without a brief —
// "I did not receive your detailed brief … Please resend it before I write
// anything." — and spent its turn on read-only recon instead. The word "done" in
// "read-only recon done meanwhile" reaped it one second later as
// bare_done_completion. None of the 🎯T395 classes caught it: it stated no
// decision fork, made no explicit claim of non-completion, and its closing
// sentence was a parenthesis of recon notes rather than a question.
//
// The missing class is the plainest one: the report ASKS FOR SOMETHING. A brief
// resend, an answer, an unblock — the worker has named an input it does not have
// and cannot continue without. That is the opposite of a finish however much
// completion vocabulary the rest of the prose carries, and it is worse than a
// stalled worker when read as a finish: a briefless worker at least sits idle in
// the fleet list, while a falsely reaped one looks like completed work.

// ReportAskClass names why a terminal report is not a finish.
type ReportAskClass int

const (
	// AskNone: nothing in the report requests the overseer's attention.
	AskNone ReportAskClass = iota
	// AskDecisionRequest: explicitly asks for a decision, go-ahead, or choice.
	AskDecisionRequest
	// AskRequest: asks the parent to SEND something the worker needs to
	// proceed — the brief, an answer, an unblock (🎯T439).
	AskRequest
	// AskExplicitIncomplete: says outright it has not finished or has not acted.
	AskExplicitIncomplete
	// AskClosingQuestion: the closing act of the report is a question.
	AskClosingQuestion
	// AskTruncated: the report arrived visibly cut (🎯T388) — what it asked for
	// may be in the missing middle, so its intent is unknown.
	AskTruncated
	// AskCheckpoint: a mid-mission checkpoint — the report declares itself one,
	// or echoes the vocabulary of the since-removed per-turn depth-ceiling
	// ask (🎯T497).
	AskCheckpoint
	// AskNoClaim: the report asserts nothing about the mission — empty body,
	// whitespace, or a content-free acknowledgement (🎯T723). Absence is not
	// a bare done. Reaping requires a positive terminal claim.
	AskNoClaim
)

func (c ReportAskClass) String() string {
	switch c {
	case AskNone:
		return "none"
	case AskDecisionRequest:
		return "decision_request"
	case AskRequest:
		return "request"
	case AskExplicitIncomplete:
		return "explicit_incomplete"
	case AskClosingQuestion:
		return "closing_question"
	case AskTruncated:
		return "truncated"
	case AskCheckpoint:
		return "checkpoint"
	case AskNoClaim:
		return "no_claim"
	default:
		return "unknown"
	}
}

// ReportAskFinding is a classification plus the evidence for it: the marker
// that matched and the span of report text it fired on (🎯T439). Offset is a
// byte offset into the report as passed, so the span can be located again.
//
// Structural classes carry no marker: AskTruncated fires on the delivery
// marker, AskClosingQuestion on the report's last lines, and both record the
// span they read rather than a phrase from a list.
type ReportAskFinding struct {
	Class  ReportAskClass
	Marker string
	Span   string
	Offset int
}

// ClassifyReportAsk reports whether a terminal report is an ask.
// A sentence is not an ask (🎯T766.4). The done answer is the envelope.
func ClassifyReportAsk(report string) ReportAskClass {
	return ClassifyReportAskDetail(report).Class
}

// ClassifyReportAskDetail keeps the structural non-claims: an empty body,
// a truncated delivery, and an exact bare ack. It does not match phrases.
func ClassifyReportAskDetail(report string) ReportAskFinding {
	s := strings.TrimSpace(report)
	if s == "" {
		// 🎯T723: an empty body is not AskNone. Silence asserts nothing.
		return ReportAskFinding{Class: AskNoClaim, Marker: "empty"}
	}
	if agentreport.IsTruncatedDelivery(s) {
		return ReportAskFinding{Class: AskTruncated, Span: truncationSpan(s)}
	}
	// 🎯T723: the harness ack is a turn-boundary artefact, not a work claim.
	// Exact-match vocabulary only. A report that merely contains the phrase
	// is not this class.
	if noopwedge.IsExactAck(s) {
		return ReportAskFinding{Class: AskNoClaim, Marker: "bare_ack", Span: s}
	}
	return ReportAskFinding{Class: AskNone}
}

// hasForwardLookingPlan is not a remaining-work decision (🎯T766.4).
// A sentence that names a next step is not a plan slot.
func hasForwardLookingPlan(string) bool { return false }

// ReportAwaitsOverseer is true when the report declares that it needs an
// answer to continue (🎯T395, 🎯T766.4). A valid escalation, a valid
// target-file request, or a blocked finish-report is that declaration.
// A sentence that asks is not. Silence is not an ask (🎯T723).
func ReportAwaitsOverseer(report string) bool {
	m, err := envelope.Parse(report)
	if err != nil || m == nil {
		return false
	}
	switch m.Kind {
	case envelope.KindEscalation, envelope.KindTargetFileRequest:
		return true
	case envelope.KindFinishReport:
		return m.IsBlocked()
	default:
		return false
	}
}

// truncationSpan is the tail of a visibly cut report — the end is where the cut
// shows, so that is what a reader needs to see in the log.
func truncationSpan(report string) string {
	span, _ := excerptAround(report, len(report), len(report))
	return span
}

// asciiLower lowercases ASCII letters only, so byte offsets into the result
// index the original report unchanged. strings.ToLower does not promise that:
// some runes change width when lowered, which would slide every span after
// them. Every marker in this file is ASCII, so nothing is lost.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// reportSpanWindow bounds the legacy clipped excerpt. Prefer matchedSentence
// for reap/ask decision spans (🎯T470): a wrong reap must be diagnosable from
// the sentence alone, not from a 200-byte window that cuts mid-clause.
const reportSpanWindow = 200

// matchedSentence returns the sentence (or newline-bounded clause) that
// contains [start,end), plus its byte offset (🎯T470). Semicolons count as
// breaks so "Implementation is complete; the oracles are missing" yields the
// clause that actually carried the claim word.
//
// Diagnostic only — never load-bearing for the reap decision itself.
func matchedSentence(report string, start, end int) (string, int) {
	if start < 0 || end > len(report) || start > end {
		return "", 0
	}
	lo := 0
	if start > 0 {
		if i := strings.LastIndexAny(report[:start], ".!?;\n"); i >= 0 {
			lo = i + 1
		}
	}
	hi := len(report)
	if i := strings.IndexAny(report[end:], ".!?;\n"); i >= 0 {
		hi = end + i + 1 // include the delimiter
		if hi <= len(report) && report[hi-1] == '\n' {
			hi-- // keep the sentence without its trailing newline
		}
	}
	for lo < start && lo < hi && isASCIISpace(report[lo]) {
		lo++
	}
	for hi > end && hi > lo && isASCIISpace(report[hi-1]) {
		hi--
	}
	for lo > 0 && lo < len(report) && !utf8.RuneStart(report[lo]) {
		lo--
	}
	for hi < len(report) && !utf8.RuneStart(report[hi]) {
		hi++
	}
	return report[lo:hi], lo
}

// excerptAround returns the span of report a reader needs to see why the
// classifier fired on [start,end): the enclosing line, clipped to
// reportSpanWindow bytes around the match, plus its offset in report.
//
// Prefer matchedSentence for new decision logging (🎯T470). Kept for callers
// that still want a bounded window (truncation tails).
func excerptAround(report string, start, end int) (string, int) {
	if start < 0 || end > len(report) || start > end {
		return "", 0
	}
	lo := strings.LastIndexByte(report[:start], '\n') + 1
	hi := len(report)
	if i := strings.IndexByte(report[end:], '\n'); i >= 0 {
		hi = end + i
	}
	if hi-lo > reportSpanWindow {
		slack := (reportSpanWindow - (end - start)) / 2
		if slack < 0 {
			slack = 0
		}
		if s := start - slack; s > lo {
			lo = s
		}
		if e := end + slack; e < hi {
			hi = e
		}
	}
	for lo > 0 && lo < len(report) && !utf8.RuneStart(report[lo]) {
		lo--
	}
	for hi < len(report) && !utf8.RuneStart(report[hi]) {
		hi++
	}
	for lo < start && lo < hi && isASCIISpace(report[lo]) {
		lo++
	}
	for hi > end && hi > lo && isASCIISpace(report[hi-1]) {
		hi--
	}
	return report[lo:hi], lo
}

func isASCIISpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

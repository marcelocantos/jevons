// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"unicode/utf8"

	"github.com/marcelocantos/jevons/internal/agentreport"
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
	default:
		return "unknown"
	}
}

// decisionRequestMarkers are blocking asks: the worker has handed a choice to
// the overseer and cannot proceed without the answer.
//
// Deliberately excludes bare politeness ("let me know if…", "happy to…"): a
// finish report that merely offers follow-up work is still a finish. Only
// phrases that put the next step in the overseer's hands are listed.
var decisionRequestMarkers = []string{
	"say go",
	"say the word",
	"your go-ahead",
	"your go ahead",
	"awaiting your",
	"awaiting a decision",
	"await your",
	"pending your",
	"holding here",
	"holding pending",
	"holding until",
	"standing by for",
	"before i proceed",
	"before proceeding",
	"will not proceed until",
	"not proceeding until",
	"which would you prefer",
	"would you prefer",
	"do you want me to",
	"should i ",
	"shall i ",
	"need a decision",
	"decision needed",
	"needs a decision",
	"your call",
	"please advise",
	"please confirm",
	"confirm before",
	"blocked pending",
	"blocked awaiting",
	"blocked on your",
	"let me know which",
	"let me know whether",
	"let me know if you'd rather",
	"say so if you'd rather",
	"question for you",
	"asking before",
}

// requestMarkers are asks for an INPUT rather than for a decision: the worker
// is missing something only its parent can supply and has said so (🎯T439).
//
// The line between these and decisionRequestMarkers is which side holds the
// work: a decision request has the worker ready to act once told which way, a
// request has it unable to act at all until something arrives.
//
// Deliberately excludes courtesies a finished worker writes on its way out —
// "ready for review", "let me know if you want the follow-up filed", "needs your
// review" — which hand nothing back to the worker. Only phrases naming an input
// the worker is waiting on are listed, so a genuine finish that politely offers
// more work still reaps (🎯T165 / 🎯T195).
var requestMarkers = []string{
	"resend",
	"re-send",
	"send it again",
	"send me the",
	"send through the",
	"please provide",
	"please supply",
	"please share",
	"please paste",
	"please forward",
	"please send",
	"please unblock",
	"unblock me",
	"please answer",
	"please clarify",
	"need clarification",
	"needs clarification",
	"need the brief",
	"need a brief",
	"need my brief",
	"without the brief",
	"without a brief",
	"missing the brief",
	"no brief",
	"did not receive",
	"didn't receive",
	"have not received",
	"haven't received",
	"never received",
	"still waiting for",
	"waiting on you",
	"waiting for your",
	"need your answer",
	"need your decision",
	"need your input",
	"need your brief",
	"need you to send",
	"i need you to",
}

// explicitIncompleteMarkers are outright statements of non-completion. They
// outrank any completion word elsewhere in the same report: a worker that says
// it has changed nothing has not finished, whatever else the prose contains.
var explicitIncompleteMarkers = []string{
	"incomplete",
	"unfinished",
	"far from done",
	"nowhere near done",
	"before changing anything",
	"before i change anything",
	"without changing anything",
	"reporting before",
	"not done",
	"not yet done",
	"i am not done",
	"not finished",
	"not yet finished",
	"nothing committed",
	"no commit yet",
	"have not committed",
	"haven't committed",
	"nothing changed yet",
	"no changes yet",
	"still working",
	"work remains",
	"mid-mission",
}

// closingQuestionTailLines is how many trailing non-empty lines count as the
// report's closing act. A question in the middle of a long evidence section is
// usually rhetorical ("why does T305 miss it?"); a question in the last breath
// is addressed to the reader.
const closingQuestionTailLines = 3

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

// ClassifyReportAsk reports whether a terminal report is asking the overseer
// for something rather than declaring the mission over. Pure string heuristic —
// the overseer's judgment still applies.
func ClassifyReportAsk(report string) ReportAskClass {
	return ClassifyReportAskDetail(report).Class
}

// ClassifyReportAskDetail is ClassifyReportAsk with the span of report text the
// decision fired on, for the reap lifecycle log.
//
// Priority runs most-specific first so the lifecycle log names a useful reason:
// a visible cut (unknowable intent) > a named missing input > an explicit
// "I have not acted" > a stated decision request > a closing question.
//
// A request outranks the rest because it is the most actionable for the parent:
// the span names what to send back.
func ClassifyReportAskDetail(report string) ReportAskFinding {
	s := strings.TrimSpace(report)
	if s == "" {
		return ReportAskFinding{Class: AskNone}
	}
	if agentreport.IsTruncatedDelivery(s) {
		return ReportAskFinding{Class: AskTruncated, Span: truncationSpan(s)}
	}
	lower := asciiLower(s)
	for _, class := range []struct {
		class   ReportAskClass
		markers []string
	}{
		{AskRequest, requestMarkers},
		{AskExplicitIncomplete, explicitIncompleteMarkers},
		{AskDecisionRequest, decisionRequestMarkers},
	} {
		if m, i := firstMarker(lower, class.markers); i >= 0 {
			span, off := excerptAround(s, i, i+len(m))
			return ReportAskFinding{Class: class.class, Marker: m, Span: span, Offset: off}
		}
	}
	if i := closingQuestionIndex(s); i >= 0 {
		span, off := excerptAround(s, i, len(s))
		return ReportAskFinding{Class: AskClosingQuestion, Span: span, Offset: off}
	}
	return ReportAskFinding{Class: AskNone}
}

// firstMarker returns the earliest of markers occurring in lower, so the span
// points at the first thing the report said rather than at whichever phrase
// happens to sit first in the list.
func firstMarker(lower string, markers []string) (string, int) {
	best, at := "", -1
	for _, m := range markers {
		i := strings.Index(lower, m)
		if i < 0 || (at >= 0 && i >= at) {
			continue
		}
		best, at = m, i
	}
	return best, at
}

// ReportAwaitsOverseer is true when the report needs an answer to continue, so
// reaping it would strand the mission (🎯T395).
func ReportAwaitsOverseer(report string) bool {
	return ClassifyReportAsk(report) != AskNone
}

// closingQuestionIndex returns the byte offset of the closing question when the
// closing act of the report is a question, else -1. Trailing markdown emphasis
// and list/quote punctuation are stripped so "**Which one?**" and "- go or
// no-go?" both count.
func closingQuestionIndex(report string) int {
	seen := 0
	lines := strings.Split(report, "\n")
	end := len(report)
	for i := len(lines) - 1; i >= 0 && seen < closingQuestionTailLines; i-- {
		start := end - len(lines[i])
		line := strings.TrimSpace(lines[i])
		end = start - 1 // the newline before this line
		if line == "" {
			continue
		}
		seen++
		if strings.HasSuffix(strings.TrimRight(line, "*_`~)\"'] \t"), "?") {
			return start + strings.Index(report[start:], line)
		}
	}
	return -1
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

// reportSpanWindow bounds the excerpt recorded for a classifier decision: long
// enough to read the sentence that fired, short enough to sit in a log line.
const reportSpanWindow = 200

// excerptAround returns the span of report a reader needs to see why the
// classifier fired on [start,end): the enclosing line, clipped to
// reportSpanWindow bytes around the match, plus its offset in report.
//
// The span is diagnostic, never load-bearing — a decision is never made from
// it, so a clipped or empty span degrades the log entry and nothing else.
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

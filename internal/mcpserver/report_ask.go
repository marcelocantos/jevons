// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"

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

// ReportAskClass names why a terminal report is not a finish.
type ReportAskClass int

const (
	// AskNone: nothing in the report requests the overseer's attention.
	AskNone ReportAskClass = iota
	// AskDecisionRequest: explicitly asks for a decision, go-ahead, or choice.
	AskDecisionRequest
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

// ClassifyReportAsk reports whether a terminal report is asking the overseer
// for something rather than declaring the mission over. Pure string heuristic —
// the overseer's judgment still applies.
//
// Priority runs most-specific first so the lifecycle log names a useful reason:
// a visible cut (unknowable intent) > an explicit "I have not acted" > a stated
// decision request > a closing question.
func ClassifyReportAsk(report string) ReportAskClass {
	s := strings.TrimSpace(report)
	if s == "" {
		return AskNone
	}
	if agentreport.IsTruncatedDelivery(s) {
		return AskTruncated
	}
	lower := strings.ToLower(s)
	for _, m := range explicitIncompleteMarkers {
		if strings.Contains(lower, m) {
			return AskExplicitIncomplete
		}
	}
	for _, m := range decisionRequestMarkers {
		if strings.Contains(lower, m) {
			return AskDecisionRequest
		}
	}
	if endsInQuestion(s) {
		return AskClosingQuestion
	}
	return AskNone
}

// ReportAwaitsOverseer is true when the report needs an answer to continue, so
// reaping it would strand the mission (🎯T395).
func ReportAwaitsOverseer(report string) bool {
	return ClassifyReportAsk(report) != AskNone
}

// endsInQuestion reports whether the closing act of the report is a question.
// Trailing markdown emphasis and list/quote punctuation are stripped so
// "**Which one?**" and "- go or no-go?" both count.
func endsInQuestion(report string) bool {
	seen := 0
	lines := strings.Split(report, "\n")
	for i := len(lines) - 1; i >= 0 && seen < closingQuestionTailLines; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		seen++
		if strings.HasSuffix(strings.TrimRight(line, "*_`~)\"'] \t"), "?") {
			return true
		}
	}
	return false
}

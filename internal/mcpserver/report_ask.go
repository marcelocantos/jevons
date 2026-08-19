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
	// AskCheckpoint: a mid-mission checkpoint — the report declares itself one,
	// or echoes the 🎯T392.4 depth-ceiling ask's own vocabulary (🎯T497).
	AskCheckpoint
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

// 🎯T446: a report that MENTIONS asking is not asking.
//
// The 🎯T439 fix kept its own author alive. jv-t439-reap-request filed two
// terminal reports declaring the mission over — commit SHA, three GREEN gate
// ids — and the daemon skipped the reap on both: once on the report's own
// description of the classifier it had just written ("a report that names an
// input it is waiting on (brief resend, an answer, an unblock) is not a
// finish"), once on the negation "I did not re-send anything". Neither hands
// anything back to the parent, so both left a finished worker sitting in the
// fleet list looking engaged — the 🎯T165 / 🎯T195 auto-deregister property
// inverted for exactly the workers most likely to write about messaging.
//
// The guards below are the mirror of 🎯T439's own move: that target narrowed
// the reap so an ask is not read as a finish, these narrow the ask so a mention
// is not read as an ask. Each is pinned by one of the two real reports, and the
// bias still favours keeping the agent — a phrase that survives all three
// guards is one the worker wrote in its own voice, unnegated, in request
// position.

// bareVerbRequestMarkers are the request phrases that are also ordinary nouns
// and gerunds: "a brief resend", "re-sending would stack a second copy". They
// count as an ask only in request position — clause-initial (an imperative), or
// after a cue that makes the clause a request. The rest of requestMarkers carry
// their own grammar ("please send", "unblock me", "did not receive") and need
// no such test.
var bareVerbRequestMarkers = map[string]bool{
	"resend":  true,
	"re-send": true,
}

// requestCues make the clause they open a request, so a bare verb after one is
// an ask rather than a mention.
var requestCues = []string{
	"please",
	"kindly",
	"can you",
	"could you",
	"would you",
	"need you to",
	"asking you to",
	"i need",
}

// negationCues turn the clause they appear in into a statement that the ask was
// NOT made. Scanned only within the marker's own clause: "I did not get
// anything, please resend" is still a request, because the negation belongs to
// the clause before the comma.
var negationCues = []string{
	"not",
	"n't",
	"never",
	"without",
	"neither",
}

// negationWindow bounds the backward scan for a negation cue. A cue further
// back than this is in a different thought even when no punctuation separates
// them.
const negationWindow = 60

// 🎯T497: a worker that checkpoints is complying, not finishing.
//
// The 🎯T392.4 depth ceiling tells an over-budget turn to "Reach a checkpoint
// and END YOUR TURN" — write down where you are, state the next step, end the
// turn, be resumed. jv-t496-owner-reply did exactly that: "Checkpoint — ending
// this turn at the depth ceiling", next steps for the successor turn, "No
// files modified yet; nothing to commit." The reap path read the progress
// header "Done so far:" as a completion claim, found "commit" in the
// next-steps prose, and deregistered the seat as finished_work with zero
// commits. The ceiling and the reaper had opposite readings of the same
// compliance, and the reaper's was destructive.
//
// Two signals, in the report's own voice:
//   - a checkpoint DECLARATION: a line that IS the word — "Checkpoint —…",
//     "Checkpoint:…", a bare CHECKPOINT banner. Prose that merely contains
//     the word ("checkpoint reports now classify…") is a mention, and a
//     finish report about checkpoint handling still reaps (the 🎯T446 lesson
//     applied to this fix's own vocabulary);
//   - a ceiling ECHO: present-tense turn-ending phrases lifted from the ask
//     itself. These may appear anywhere — nobody writes "ending this turn"
//     about somebody else's turn.
var checkpointMarkers = []string{
	"ending this turn",
	"ending my turn",
	"ending the turn",
	"at the depth ceiling",
	"hit the depth ceiling",
	"reached the depth ceiling",
	"depth-ceiling checkpoint",
	"depth ceiling checkpoint",
	"t392.4 depth ceiling",
}

// checkpointDelimiters are what may follow the word in a declaration line:
// nothing, or a punctuation break. A letter would make it a longer word
// ("checkpoints") and a space-plus-word would make it ordinary prose
// ("Checkpoint handling fixed").
var checkpointDelimiters = []string{":", "—", "–", "-", ".", ",", ";"}

// explicitIncompleteMarkers are outright statements of non-completion. They
// outrank any completion word elsewhere in the same report: a worker that says
// it has changed nothing has not finished, whatever else the prose contains.
//
// 🎯T470: the jv-t390 / jv-t391 checkpoint incidents ended with explicit
// non-completion ("nothing here is achieved" / "oracles are missing") while
// still carrying a bare "complete" substring — those sentences must veto.
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
	"nothing to commit",
	"no files modified",
	"status: in progress",
	"still in progress",
	"no commit yet",
	"no commit, no gate",
	"no product evidence yet",
	"nothing here is achieved",
	"oracles are missing",
	"oracles are the missing half",
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
	// A checkpoint declaration outranks the marker classes: a report that names
	// its own genre has answered the question the rest of this function is
	// guessing at (🎯T497).
	if f, ok := checkpointDeclaration(s, lower); ok {
		return f
	}
	for _, class := range []struct {
		class   ReportAskClass
		markers []string
		accept  func(lower, marker string, at int) bool
	}{
		{AskRequest, requestMarkers, isRequestAsk},
		{AskCheckpoint, checkpointMarkers, nil},
		{AskExplicitIncomplete, explicitIncompleteMarkers, nil},
		{AskDecisionRequest, decisionRequestMarkers, nil},
	} {
		if m, i := firstAcceptedMarker(lower, class.markers, class.accept); i >= 0 {
			// 🎯T470: name the sentence that matched, not a 200-byte window.
			span, off := matchedSentence(s, i, i+len(m))
			return ReportAskFinding{Class: class.class, Marker: m, Span: span, Offset: off}
		}
	}
	if i := closingQuestionIndex(s); i >= 0 {
		span, off := matchedSentence(s, i, len(s))
		return ReportAskFinding{Class: AskClosingQuestion, Span: span, Offset: off}
	}
	return ReportAskFinding{Class: AskNone}
}

// firstAcceptedMarker returns the earliest occurrence of any marker that accept
// admits, or ("", -1). A nil accept admits every occurrence, which is
// firstMarker.
//
// A rejected occurrence never vetoes the report: the scan continues past it, so
// "I did not re-send anything — please send the brief" still classifies on the
// second phrase (🎯T446).
func firstAcceptedMarker(lower string, markers []string, accept func(lower, marker string, at int) bool) (string, int) {
	best, at := "", -1
	for _, m := range markers {
		for from := 0; from <= len(lower)-len(m); {
			i := strings.Index(lower[from:], m)
			if i < 0 {
				break
			}
			i += from
			if at >= 0 && i >= at {
				break // a later occurrence of this marker cannot win
			}
			if accept == nil || accept(lower, m, i) {
				best, at = m, i
				break
			}
			from = i + 1
		}
	}
	return best, at
}

// firstMarker returns the earliest of markers occurring in lower, so the span
// points at the first thing the report said rather than at whichever phrase
// happens to sit first in the list.
func firstMarker(lower string, markers []string) (string, int) {
	return firstAcceptedMarker(lower, markers, nil)
}

// isRequestAsk decides whether a request marker at `at` is the worker actually
// asking for an input, rather than naming the phrase in passing (🎯T446).
//
// Three guards, each pinned by a real false positive:
//   - the match is a whole phrase, not the head of a longer word, so the gerund
//     in "re-sending would stack a second copy" is not the verb in "re-send it";
//   - its own clause does not negate it, so "I did not re-send anything" is not
//     a request to re-send;
//   - a bare verb sits in request position — clause-initial or after a request
//     cue — so "(brief resend, an answer, an unblock)" is vocabulary, not a
//     demand.
func isRequestAsk(lower, marker string, at int) bool {
	if !wholePhraseAt(lower, marker, at) {
		return false
	}
	clause := clauseStart(lower, at)
	if hasCueWord(lower[clause:at], negationCues) {
		return false
	}
	if !bareVerbRequestMarkers[marker] {
		return true
	}
	before := lower[clause:at]
	return strings.TrimLeft(before, " \t*_`>-#") == "" || hasCueWord(before, requestCues)
}

// wholePhraseAt is true when the marker at `at` is not glued to a longer word on
// either side. Hyphens count as word characters here, so "re-send" does not
// match inside "re-sending" and "resend" does not match inside "resends".
func wholePhraseAt(lower, marker string, at int) bool {
	if at > 0 && isPhraseByte(lower[at-1]) {
		return false
	}
	end := at + len(marker)
	return end >= len(lower) || !isPhraseByte(lower[end])
}

func isPhraseByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '\''
}

// clauseStart is the offset of the start of the clause containing `at`, bounded
// by negationWindow. Commas end a clause as firmly as full stops do here: in "I
// did not get anything, please resend" the negation belongs to the clause
// before the comma, and reading it as this clause's would reject a plain
// request.
func clauseStart(lower string, at int) int {
	lo := at - negationWindow
	if lo < 0 {
		lo = 0
	}
	if i := strings.LastIndexAny(lower[lo:at], ".,;:!?\n()"); i >= 0 {
		return lo + i + 1
	}
	return lo
}

// hasCueWord is true when s contains any cue as a whole word. Substring matching
// would find "not" in "nothing" and "cannot".
func hasCueWord(s string, cues []string) bool {
	for _, cue := range cues {
		for from := 0; from <= len(s)-len(cue); {
			i := strings.Index(s[from:], cue)
			if i < 0 {
				break
			}
			i += from
			if wholePhraseAt(s, cue, i) {
				return true
			}
			from = i + 1
		}
	}
	return false
}

// checkpointDeclaration finds a line that declares the report a checkpoint
// (🎯T497): its decoration-stripped text is the word "checkpoint" alone, or the
// word followed by a punctuation break — "Checkpoint — ending this turn…",
// "Checkpoint: parser mapped", a bare CHECKPOINT banner. Line-initial position
// plus the delimiter is what separates a declaration from a mention: prose that
// contains the word mid-sentence, or uses it as an adjective ("checkpoint
// reports"), never matches.
func checkpointDeclaration(s, lower string) (ReportAskFinding, bool) {
	const word = "checkpoint"
	for start := 0; start < len(lower); {
		end := strings.IndexByte(lower[start:], '\n')
		if end < 0 {
			end = len(lower)
		} else {
			end += start
		}
		line := lower[start:end]
		at := start + len(line) - len(strings.TrimLeft(line, " \t*_`>-#"))
		if strings.HasPrefix(lower[at:end], word) && isCheckpointDelimited(lower[at+len(word):end]) {
			span, off := matchedSentence(s, at, at+len(word))
			return ReportAskFinding{Class: AskCheckpoint, Marker: word, Span: span, Offset: off}, true
		}
		start = end + 1
	}
	return ReportAskFinding{}, false
}

// isCheckpointDelimited is true when rest — the line after the word — starts
// with a break rather than more word. Trailing emphasis is stripped first so
// "**CHECKPOINT**" is still bare.
func isCheckpointDelimited(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	rest = strings.TrimRight(rest, " \t*_`~")
	if rest == "" {
		return true
	}
	for _, d := range checkpointDelimiters {
		if strings.HasPrefix(rest, d) {
			return true
		}
	}
	return false
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

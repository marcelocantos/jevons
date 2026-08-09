// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ownerqa decides one thing: did an owner question get an
// owner-visible answer (🎯T378).
//
// The 🎯T355 owner-interaction loop already keeps a reply owed on every owner
// turn, but it credits satisfaction on a *seal* — internal/server/server.go
// calls NoteOwnerReplySealed on any owner-turn terminal stop, whether or not
// the turn painted a single character the owner could read. That is exactly
// the shape session 019fe5e8 died in: the owner asked why Orthograph was
// handing Pippa a 127.0.0.1 address, the overseer escalated, got the answer,
// wrote "Relay to owner briefly with re-pair steps" in its own reasoning, and
// then emitted ~100 consecutive no-op turns with zero owner-visible text. Every
// one of those turns sealed. Every seal satisfied reply-or-residual. Nothing
// anywhere asserted the question had been answered.
//
// # The distinction this package exists to hold
//
// [silent] is legitimate. Routine status and resume turns are answered [silent]
// constantly, and a detector that merely counted silent turns or flagged no-op
// streaks would fire all day on healthy supervision traffic and be switched off
// within a day. So silence is judged against what the owner actually said:
//
//	owner asked a question  → silence is never a legitimate answer
//	owner said anything else → silence is fine, and not this package's business
//
// That gate is the whole design. It is why IsQuestion is deliberately tight
// (see its doc) rather than generous: a false positive here is a detector that
// nags about turns the owner never wanted an answer to, which is the failure
// mode that gets a detector deleted.
//
// # What "answered" means
//
// Owner-visible assistant prose, or a named residual. A seal is not an answer,
// an empty end_turn is not an answer, and a [silent] body is not an answer.
// A visible error frame *is* an outcome — the owner can read it and knows
// where the turn went — so it closes the question as a residual rather than
// leaving it standing.
//
// # Boundary with 🎯T388
//
// T388 covers reports that arrive truncated mid-sentence, losing the tail that
// carried the asks. That is the same family — intent evaporating with nothing
// asserting it arrived — one layer down, and the two compose rather than
// overlap: a truncated report still paints owner-visible prose, so this
// package counts the question answered and stays quiet, while T388's predicate
// is the one with something to say about the missing tail. Nothing here should
// grow an opinion about reply *completeness*; this dimension asks only whether
// anything owner-visible arrived at all.
//
// The package is pure: no I/O, no clock beyond what the caller passes.
// Scan reads a fixture (or real) chatlog for the hermetic oracle; the live
// daemon path feeds the same predicate through internal/converge's
// question_answered dimension.
package ownerqa

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
)

// DefaultNoOpTurns is how many consecutive zero-owner-visible-text overseer
// turns, *with an owner question still outstanding*, read as a degenerate
// output loop rather than as ordinary quiet work (🎯T378 acceptance 3).
//
// The qualifier is load-bearing. Counting no-op turns on their own describes
// healthy supervision — the overseer works the fleet in silence for long
// stretches by design. Counting them only while the owner is owed an answer
// describes 019fe5e8 and essentially nothing else.
const DefaultNoOpTurns = 3

// Kind is why a question is being reported.
type Kind string

const (
	// KindUnanswered: the owner asked, the turn ended, nothing owner-visible
	// was ever painted.
	KindUnanswered Kind = "owner_question_unanswered"
	// KindNoOpLoop: unanswered, and the overseer burned at least
	// DefaultNoOpTurns consecutive turns producing no owner-visible text —
	// the 019fe5e8 shape. Strictly louder than KindUnanswered, never a
	// separate condition: a no-op loop is always also an unanswered question.
	KindNoOpLoop Kind = "owner_question_noop_loop"
)

// Finding is one owner question that never got an owner-visible answer.
type Finding struct {
	// Line is the 0-based index of the owner's question in the scanned slice.
	Line int
	// Question is the owner's text, as they typed it.
	Question string
	// Kind distinguishes a plain unanswered question from a no-op loop.
	Kind Kind
	// NoOpTurns is how many zero-owner-visible-text overseer turns elapsed
	// while this question stood open.
	NoOpTurns int
}

// IsQuestion reports whether owner-typed text asks something that owes an
// owner-visible answer.
//
// Deliberately tight. The cost of a false negative is one missed detection;
// the cost of a false positive is a detector that fires on routine traffic
// and gets turned off, which costs every future detection. Two rules:
//
//	a '?' ends some line          — "haven't we integrated pigeon?"
//	the text opens with a wh-word — "what happened to the relay"
//
// The wh-word list holds only unambiguous interrogatives. Auxiliary openers
// (is/are/do/can/should/will/have) are excluded on purpose: they collide with
// declaratives and directives ("should be fine", "can you fix the build"), and
// when the owner genuinely asks in that form they write the '?' anyway, so
// rule one already has them.
//
// Directives are not questions. "fix the build", "restart the daemon", "ship
// it", "continue" own no answer beyond the work itself, and this package makes
// no claim about them.
func IsQuestion(text string) bool {
	t := strings.TrimSpace(stripUserMarker(text))
	if t == "" {
		return false
	}
	for line := range strings.SplitSeq(t, "\n") {
		if endsWithQuestionMark(line) {
			return true
		}
	}
	return opensInterrogative(t)
}

// endsWithQuestionMark reports whether a line terminates in '?', ignoring
// trailing quotes, brackets and punctuation the owner may have closed with.
func endsWithQuestionMark(line string) bool {
	trimmed := strings.TrimRightFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(`"')]}»`+"`*_", r)
	})
	return strings.HasSuffix(trimmed, "?")
}

// interrogativeOpeners are the wh-words that reliably open a question even
// when the owner omits the '?'. Kept short on purpose — see IsQuestion.
var interrogativeOpeners = []string{
	"what", "why", "how", "when", "where", "who", "whom", "which", "whose",
}

// opensInterrogative reports whether the text's first word is a wh-word.
func opensInterrogative(t string) bool {
	// Skip any leading markdown/quote decoration the owner pasted.
	t = strings.TrimLeft(t, "> *_#-\t ")
	first := t
	if i := strings.IndexFunc(t, func(r rune) bool {
		return unicode.IsSpace(r) || r == '\'' || r == '’'
	}); i >= 0 {
		first = t[:i]
	}
	first = strings.ToLower(strings.Trim(first, `"'.,;:!`))
	return slices.Contains(interrogativeOpeners, first)
}

// stripUserMarker removes the internal owner-turn marker the daemon prepends
// when delivering to a provider (internal/server.userTurnPrefix). The marker
// is transport, never something the owner typed, so the predicate must not see
// it — and must not depend on the server package to say so.
func stripUserMarker(text string) string {
	const marker = "[user]"
	t := strings.TrimLeft(text, " \t")
	if !strings.HasPrefix(t, marker) {
		return text
	}
	return strings.TrimPrefix(t, marker)
}

// wireLine is the subset of a chatlog JSONL record this package reads. The
// shapes are internal/server's: typed content blocks since 🎯T384, with the
// legacy bare string still accepted because journals on disk predate it.
type wireLine struct {
	Type       string `json:"type"`
	TurnOrigin string `json:"turn_origin"`
	Text       string `json:"text"`
	Error      string `json:"error"`
	Message    struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentText flattens message.content, accepting both the typed-block shape
// and the legacy bare string.
func (w wireLine) contentText() string {
	if len(w.Message.Content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(w.Message.Content, &s) == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(w.Message.Content, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.Text)
	}
	return b.String()
}

// role classifies one journal line for the scan.
type role int

const (
	roleIgnore role = iota
	// roleOwnerTurn: the owner spoke. Opens a new question window.
	roleOwnerTurn
	// roleVisibleReply: owner-visible assistant prose. Answers.
	roleVisibleReply
	// roleResidual: a visible outcome that is not prose (an error frame).
	// Closes the question honestly rather than leaving it standing.
	roleResidual
	// roleNoOpTurn: an overseer turn that sealed without painting anything —
	// an empty end_turn, or a [silent] body the owner never saw.
	roleNoOpTurn
)

// classify maps one raw journal line to its role in the scan.
func classify(line string) (role, string) {
	var w wireLine
	if json.Unmarshal([]byte(line), &w) != nil {
		return roleIgnore, ""
	}
	switch w.Type {
	case "user":
		// Agent/system notifications ride the user role too (🎯T381). Only
		// what the owner typed opens a question. Absent origin means owner:
		// chatUserEchoAs defaults that way, and every journal predating the
		// field is owner text.
		if w.TurnOrigin != "" && w.TurnOrigin != "owner" {
			return roleIgnore, ""
		}
		return roleOwnerTurn, w.contentText()
	case "assistant":
		if strings.TrimSpace(w.contentText()) == "" {
			return roleNoOpTurn, ""
		}
		return roleVisibleReply, w.contentText()
	case "error":
		return roleResidual, w.Error
	default:
		// agent_note, status, and every other frame: not owner-visible prose,
		// and not a turn the owner is waiting on.
		return roleIgnore, ""
	}
}

// Scan walks chatlog JSONL lines in order and reports every owner question
// that never received an owner-visible answer.
//
// A question window opens on an owner turn that IsQuestion, and closes on the
// first owner-visible reply, on a residual, or on the next owner turn. A
// window still open at the end of the log is reported: the owner is still
// waiting, which is precisely the state 019fe5e8 handed over in.
//
// noOpThreshold is the DefaultNoOpTurns gate; pass <= 0 to take the default.
func Scan(lines []string, noOpThreshold int) []Finding {
	if noOpThreshold <= 0 {
		noOpThreshold = DefaultNoOpTurns
	}
	var out []Finding
	// open is the question awaiting an answer, or nil.
	var open *Finding

	closeOpen := func() {
		if open == nil {
			return
		}
		if open.NoOpTurns >= noOpThreshold {
			open.Kind = KindNoOpLoop
		}
		out = append(out, *open)
		open = nil
	}

	for i, line := range lines {
		r, text := classify(line)
		switch r {
		case roleOwnerTurn:
			// A new owner turn supersedes the last question: whatever the
			// overseer owed, the owner has moved on and the answer never came.
			closeOpen()
			if IsQuestion(text) {
				open = &Finding{Line: i, Question: strings.TrimSpace(stripUserMarker(text)), Kind: KindUnanswered}
			}
		case roleVisibleReply, roleResidual:
			// Answered (or honestly closed). Nothing to report.
			open = nil
		case roleNoOpTurn:
			if open != nil {
				open.NoOpTurns++
			}
		}
	}
	closeOpen()
	return out
}

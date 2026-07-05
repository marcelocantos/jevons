// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import "fmt"

// Integrity issues are "obviously wrong" corruption signatures in a
// thread's user turns — the kind of defect a machine can flag WITHOUT
// human judgment (oracle-first: a decidable invariant, not a taste
// call). They exist to catch send/PTY-layer bugs (e.g. a recalled
// message typed on top of residual input, producing a doubled turn)
// automatically, rather than relying on the owner noticing a mess.

// IssueKind enumerates the detectable corruption signatures.
type IssueKind string

const (
	// IssueDuplicateTurn: a user turn identical to the one before it.
	IssueDuplicateTurn IssueKind = "duplicate-turn"
	// IssueSelfConcatenation: a user turn whose text is (mostly) its own
	// leading half repeated — the signature of the same input typed
	// twice into the box before submit.
	IssueSelfConcatenation IssueKind = "self-concatenation"
)

// IntegrityIssue is one detected pathology, located by its position
// among the user turns.
type IntegrityIssue struct {
	Kind      IssueKind
	TurnIndex int    // 0-based index among user turns
	Detail    string // human-readable specifics
}

// minRepeat is the shortest doubled prefix worth flagging — below this,
// short naturally-repetitive text ("no no", "go go") would false-positive.
const minRepeat = 12

// CheckUserTurns scans a transcript tail for corruption in the user
// prompts. It is a pure function of the entries, so it doubles as a CI
// oracle over captured transcripts and as a live thread-health signal
// the butler can surface ("this thread's last turn looks corrupted").
func CheckUserTurns(entries []Entry) []IntegrityIssue {
	var issues []IntegrityIssue
	var prev string
	idx := 0
	for _, e := range entries {
		if !e.IsUserTurn || e.Text == "" {
			continue
		}
		text := e.Text
		if idx > 0 && text == prev {
			issues = append(issues, IntegrityIssue{
				Kind:      IssueDuplicateTurn,
				TurnIndex: idx,
				Detail:    "identical to the previous user turn",
			})
		}
		if h := doubledPrefixLen(text); h > 0 {
			issues = append(issues, IntegrityIssue{
				Kind:      IssueSelfConcatenation,
				TurnIndex: idx,
				Detail:    fmt.Sprintf("text repeats its first %d bytes back-to-back", h),
			})
		}
		prev = text
		idx++
	}
	return issues
}

// doubledPrefixLen returns h > 0 when text begins with a substantial
// doubled prefix (text[:h] == text[h:2h]) that covers most of the
// message — the "typed twice before submit" signature. Returns 0
// otherwise. Byte-wise comparison is sufficient: a genuine double is
// byte-identical, so a mid-rune split simply fails to match.
func doubledPrefixLen(text string) int {
	n := len(text)
	for h := n / 2; h >= minRepeat; h-- {
		// The repeated region must cover a majority of the message, so a
		// message that merely opens with a repeated phrase isn't flagged.
		if 2*h*5 >= n*3 && text[:h] == text[h:2*h] {
			return h
		}
	}
	return 0
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

// Reply-shape bounds for ClassifyReply. A provider failure that arrives as
// the whole turn is short and bare; real work prose is neither.
const (
	maxReplyFailureChars = 240
	maxReplyFailureLines = 3
)

// ClassifyReply classifies a whole agent reply as a provider failure (🎯T283).
//
// The owner chat path classifies individual streamed events
// (server/chat_wire.go), where a bare "Internal error" arrives on its own
// event. A direct/deliver caller instead sees the aggregated reply for the
// turn, so applying ClassifyText there would rewrite honest work prose —
// "fixed the timeout bug" contains a backend marker, "auth error handling
// added" contains an auth marker. ClassifyReply therefore fires only when the
// reply is *nothing but* a failure signal, and returns ClassNone for anything
// that reads as work.
//
// Residual: a worker whose genuine output is one short failure-shaped line
// still classifies as a provider failure. The error arm (Classify on a
// returned error) is the precise signal; this is the best available reading
// of a reply-carried outage.
func ClassifyReply(reply string) Class {
	s := strings.TrimSpace(reply)
	if s == "" {
		return ClassNone
	}
	low := strings.ToLower(s)

	// A whole-message "Internal error" is unambiguous at any length bound.
	if isBareInternalError(low) {
		return ClassBackendUnavailable
	}
	if isBusyMessage(low) {
		return ClassNone
	}
	if len([]rune(s)) > maxReplyFailureChars || countNonEmptyLines(s) > maxReplyFailureLines {
		return ClassNone
	}
	// A reply that reports work is work, however many failure words it uses.
	if containsAny(low, workMarkers...) {
		return ClassNone
	}
	class := ClassifyText(s)
	// The residual "contains the word error" bucket is not proof of an outage;
	// only an explicitly matched class may speak for a whole turn.
	if class == ClassUnknown {
		return ClassNone
	}
	return class
}

// ReplyFailure returns the class and owner-visible copy for a reply that is
// nothing but a provider failure. ok is false when the reply is real work.
func ReplyFailure(reply string) (class Class, ownerMsg string, ok bool) {
	class = ClassifyReply(reply)
	if !class.IsFailure() {
		return ClassNone, "", false
	}
	return class, OwnerCopy(class, reply), true
}

func countNonEmptyLines(s string) int {
	n := 0
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// workMarkers are phrases that mean the agent is reporting on work it did.
// Their presence vetoes a whole-reply failure reading: an agent that says it
// fixed a timeout is not a timeout.
var workMarkers = []string{
	"done", "completed", "complete", "finished",
	"fixed", "added", "created", "wrote", "updated", "removed",
	"implemented", "landed", "committed", "commit ",
	"passed", "passing", "green", "success",
	"i ran", "i've", "i have", "here's", "here is",
	"✅", "```",
}

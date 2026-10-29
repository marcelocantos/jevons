// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "testing"

const trailingFinish = "Sidecar fix is landed. Gate is green.\n\n" +
	"| Check | Result |\n|---|---|\n| gate | GREEN |\n\n" +
	"```jevons\n" +
	"jevons: kind finish-report\n" +
	"jevons: target T1186\n" +
	"jevons: oracle sha=2ca7a7d4186c5d83b5fac4c467d1da9c192892b7\n" +
	"jevons: silent-ledger none\n" +
	"```\n"

// The shape Cursor workers actually write: prose first, envelope last
// (2026-10-22 jv-t1186-finish-reap). It is typed and reads as done.
func TestTrailingEnvelopeIsTyped(t *testing.T) {
	m, err := Parse(trailingFinish)
	if err != nil || m == nil {
		t.Fatalf("trailing envelope not parsed: m=%v err=%v", m, err)
	}
	if m.Kind != KindFinishReport || m.AtLine1 {
		t.Fatalf("kind=%q atLine1=%v", m.Kind, m.AtLine1)
	}
	if m.Payload == "" || m.Payload[:8] != "Sidecar " {
		t.Fatalf("payload should be the prose before the envelope: %q", m.Payload)
	}
	if got := ClassifyDone(trailingFinish); got != DoneYes {
		t.Fatalf("ClassifyDone = %v, want DoneYes", got)
	}
}

// An envelope with prose after it may be a quotation; it stays unknown.
func TestMidMessageEnvelopeStaysUnknown(t *testing.T) {
	mid := trailingFinish + "\nThat was the other seat's report, not mine.\n"
	if m, _ := Parse(mid); m != nil {
		t.Fatalf("mid-message envelope parsed: %+v", m)
	}
	if got := ClassifyDone(mid); got == DoneYes {
		t.Fatal("mid-message envelope classified done")
	}
}

// A final fenced block in another language is not an envelope.
func TestTrailingOtherFenceIsNotAnEnvelope(t *testing.T) {
	if m, _ := Parse("Here is the diff:\n\n```go\nfunc f() {}\n```\n"); m != nil {
		t.Fatalf("go fence parsed as envelope: %+v", m)
	}
}

// Line 1 still wins and still says so.
func TestLine1EnvelopeUnchanged(t *testing.T) {
	m, err := Parse("```jevons\njevons: kind finish-report\njevons: target T1\njevons: oracle sha=2ca7a7d4186c5d83b5fac4c467d1da9c192892b7\njevons: silent-ledger none\n```\nprose\n")
	if err != nil || m == nil || !m.AtLine1 || m.Payload != "prose\n" {
		t.Fatalf("line-1 envelope: m=%+v err=%v", m, err)
	}
}

// Claudia's goal protocol appends GOAL_STATUS after the envelope; that line
// is a protocol token, so the envelope still ends the message.
func TestTrailingEnvelopeBeforeGoalStatus(t *testing.T) {
	msg := trailingFinish + "\nGOAL_STATUS: complete\n"
	if got := ClassifyDone(msg); got != DoneYes {
		t.Fatalf("ClassifyDone = %v, want DoneYes", got)
	}
	if m, _ := Parse(trailingFinish + "\n**Prose:** more words.\n\nGOAL_STATUS: complete\n"); m != nil {
		t.Fatalf("prose between envelope and GOAL_STATUS parsed: %+v", m)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T447 clause 2 — the four sends the overseer read wrongly on 2026-08-14,
// replayed from their own bytes.
//
// jevons-po's session 1b03ec0a-e6d7-4459-a46e-63672ff78e10 carried four
// overseer sends in one window. One was enqueued, dequeued and landed as an
// authored user message. The other three were enqueued, removed, and replayed
// into the running turn as `queued_command` attachments — and never became
// user messages, because a queued message never does.
//
// The fixture is that region of the real file with the assistant turns and
// tool results trimmed out; every record it keeps is verbatim, and each
// payload is kept in a paired .txt so the needle under test is the text that
// was actually sent rather than a retyping of it.
//
// The reading the overseer ran is here too, as t447UserMessageOnly, over the
// same bytes. That is what makes this a regression fixture rather than a
// green-on-the-new-predicate tautology: the documented instrument set reaches
// LOST on three messages the receiver was demonstrably holding.

func t447Fixture(t *testing.T, session, payload string) (path, needle string) {
	t.Helper()
	path = filepath.Join("testdata", session+".jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", payload+"_payload.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return path, Needle(string(raw))
}

const t447Session = "t447_1b03ec0a_four_sends"

func TestT447TheFourSendsReadAsDeliveredOrWaitingAndNeverAsLost(t *testing.T) {
	for _, tc := range []struct {
		payload string
		want    Reading
		because string
	}{{
		payload: "t447_1b03ec0a_send1_delivered",
		want:    ReadingDelivered,
		because: "enqueued behind a live turn, dequeued at the boundary, and then " +
			"landed as an authored user message of 5315 bytes. This is the one the " +
			"overseer re-sent verbatim as a 'first delivery, not a duplicate'.",
	}, {
		payload: "t447_1b03ec0a_send2_waiting",
		want:    ReadingInFlight,
		because: "enqueue, remove, and a queued_command attachment carrying the " +
			"payload. No user message carries it and none ever will — that is the " +
			"terminal shape of a message accepted behind a live turn, not a hole.",
	}, {
		payload: "t447_1b03ec0a_send3_waiting",
		want:    ReadingInFlight,
		because: "the 6852-byte send. Same shape, and the one reported to a product " +
			"owner as a specimen of a defect class it is not an instance of.",
	}, {
		payload: "t447_1b03ec0a_send4_waiting",
		want:    ReadingInFlight,
		because: "the fourth, still waiting when the window was read. Waiting is a " +
			"state the instrument now has a name for.",
	}} {
		t.Run(tc.payload, func(t *testing.T) {
			path, needle := t447Fixture(t, t447Session, tc.payload)
			// from=0, hadTranscript=false: the whole region, read at any later
			// time. A reading is not a race against a timer.
			got := Scan(path, 0, false, needle).Reading()
			if got != tc.want {
				t.Fatalf("reading=%s want %s — %s", got, tc.want, tc.because)
			}
			if got == ReadingLost {
				t.Fatal("a message the receiver holds was diagnosed as lost")
			}
			if got.PermitsResend() {
				t.Fatalf("reading=%s permits a re-send of a message the receiver already has "+
					"(clause 4: a re-send stacks a duplicate, a flush submits the whole backlog)", got)
			}
		})
	}
}

// The control, and it is the load-bearing half: the fix must not be reached by
// declaring everything in-flight. Same fixture, same real payload, with the
// three records that carry it removed — a genuinely dropped message, in a file
// that is otherwise identical and still full of other people's queue records.
func TestT447AGenuinelyDroppedPayloadStillReadsAsLost(t *testing.T) {
	path, needle := t447Fixture(t, "t447_1b03ec0a_dropped_control", "t447_1b03ec0a_send3_waiting")
	got := Scan(path, 0, false, needle).Reading()
	if got != ReadingLost {
		t.Fatalf("reading=%s want lost — the payload is in no record of this file, and an "+
			"instrument that answers in_flight here has stopped being able to report a loss", got)
	}
	if !got.PermitsResend() {
		t.Fatal("lost is the one reading on which a re-send is a candidate")
	}
	if got.Held() {
		t.Fatal("a dropped message was reported as held by the receiver")
	}
}

// The reading that produced the false loss report, run over the same bytes.
// Faithful application of 🎯T416's documented instruments reaches "nothing" on
// three messages the receiver was holding — which is the defect, not operator
// error, and is why the positive test had to be named.
func TestT447TheDocumentedInstrumentsAloneReachLostOnAllThree(t *testing.T) {
	for _, payload := range []string{
		"t447_1b03ec0a_send2_waiting",
		"t447_1b03ec0a_send3_waiting",
		"t447_1b03ec0a_send4_waiting",
	} {
		t.Run(payload, func(t *testing.T) {
			path, needle := t447Fixture(t, t447Session, payload)
			if t447UserMessageOnly(t, path, needle) {
				t.Fatal("fixture no longer carries this payload solely in queue records; " +
					"it has stopped reproducing the false loss report")
			}
			if got := Scan(path, 0, false, needle).Reading(); got != ReadingInFlight {
				t.Fatalf("reading=%s — the instrument inherited the blind spot it exists to fix", got)
			}
		})
	}

	// And the delivered one is not rescued by the queue records being read:
	// payload-match at user-message level gets that one right on its own. The
	// gap is specifically about waiting, which is what makes it invisible.
	path, needle := t447Fixture(t, t447Session, "t447_1b03ec0a_send1_delivered")
	if !t447UserMessageOnly(t, path, needle) {
		t.Fatal("fixture no longer carries the delivered payload as an authored user message")
	}
}

// t447UserMessageOnly is the reading that was run on the night: authored user
// messages, nothing else.
func t447UserMessageOnly(t *testing.T, path, needle string) bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		rec, ok := Decode([]byte(line))
		if ok && rec.Kind == KindUserMessage && strings.Contains(Normalize(rec.Text), needle) {
			return true
		}
	}
	return false
}

// The four readings, pinned against the fates they are derived from. Cheap, and
// it is the sentence the whole target turns on: an unread region is not an
// absence, and an absence is the only thing a re-send may act on.
func TestT447ReadingsAreDerivedFromFatesNotMeasuredSeparately(t *testing.T) {
	for _, tc := range []struct {
		fate   Fate
		want   Reading
		held   bool
		resend bool
	}{
		{FateUserMessage, ReadingDelivered, true, false},
		{FateEnteredTurn, ReadingInFlight, true, false},
		{FateQueued, ReadingInFlight, true, false},
		{FateUnseen, ReadingLost, false, true},
		{FateUnknown, ReadingUndecided, false, false},
	} {
		got := tc.fate.Reading()
		if got != tc.want {
			t.Errorf("fate=%s reading=%s want %s", tc.fate, got, tc.want)
		}
		if got.Held() != tc.held {
			t.Errorf("fate=%s held=%v want %v", tc.fate, got.Held(), tc.held)
		}
		if got.PermitsResend() != tc.resend {
			t.Errorf("fate=%s permits_resend=%v want %v", tc.fate, got.PermitsResend(), tc.resend)
		}
	}
}

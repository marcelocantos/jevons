// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownergate

import (
	"strings"
	"testing"
	"time"
)

func testNow() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) }

// The refusal is the product (🎯T449 clause 4). Recording this state removes a
// target from unattended consumption, so a claim that names nothing a reader
// can go and check must not be writable — otherwise the state becomes the
// cheapest way for an agent under completion pressure to make unfinished work
// stop being swept.
func TestReasonRefusesClaimWithoutLandedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rec      Record
		wantWord string
	}{
		{
			name:     "no evidence at all",
			rec:      Record{Question: "Does the aside selection stay put after a hard reload?"},
			wantWord: "evidence",
		},
		{
			name: "evidence is a bare completion claim",
			rec: Record{
				Question: "Does the aside selection stay put after a hard reload?",
				Evidence: "done, all green",
			},
			wantWord: "evidence",
		},
		{
			name:     "no question to answer",
			rec:      Record{Evidence: "landed at 8f25010, GATE node id=adf36ad8"},
			wantWord: "question",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.rec.Reason()
			if err == nil {
				t.Fatalf("Reason() accepted an unbacked claim: %q", got)
			}
			if !strings.Contains(err.Error(), tc.wantWord) {
				t.Fatalf("error %q does not name the missing part %q", err, tc.wantWord)
			}
		})
	}
}

func TestReasonAcceptsEachCurrencyOfLandedEvidence(t *testing.T) {
	for _, ev := range []string{
		"code landed at 8f25010 (ancestor of HEAD)",
		"GATE node id=adf36ad8 GREEN",
		"TestT449AwaitingOwnerVerdictParks green",
	} {
		rec := Record{
			Question:   "Does the selection survive a hard reload?",
			Evidence:   ev,
			RecordedBy: "jevons-po",
			Now:        testNow(),
		}
		reason, err := rec.Reason()
		if err != nil {
			t.Fatalf("evidence %q refused: %v", ev, err)
		}
		if !strings.Contains(reason, MarkerAwaiting) {
			t.Fatalf("reason does not open with the marker: %q", reason)
		}
		if !strings.Contains(reason, "jevons-po") || !strings.Contains(reason, "2026-08-15") {
			t.Fatalf("reason drops who/when: %q", reason)
		}
		if !strings.Contains(reason, ev) {
			t.Fatalf("reason drops the evidence it was given: %q", reason)
		}
		// The whole point: what the writer writes, the reader reads.
		if !IsAwaiting(OwnerHandle, reason, "") {
			t.Fatalf("writer and reader disagree about %q", reason)
		}
	}
}

// An answered gate is no longer a gate. Reject in particular must return the
// leaf to normal handling — work resumes from the landed commit, and waiting
// for the assignment to be unwound first would re-create the original bug
// with the roles swapped.
func TestAnsweredGateIsNotAwaiting(t *testing.T) {
	rec := Record{
		Question:   "Does it still jump?",
		Evidence:   "landed at 8f25010",
		RecordedBy: "jevons-po",
		Now:        testNow(),
	}
	reason, err := rec.Reason()
	if err != nil {
		t.Fatal(err)
	}
	if !IsAwaiting(OwnerHandle, reason, "") {
		t.Fatal("fresh record is not awaiting")
	}
	for _, v := range []Verdict{VerdictAccept, VerdictReject} {
		answered := reason + " " + FormatAnswer(v, "still jumps on the second aside", "jevons-po", testNow())
		if !IsAnswered(answered, "") {
			t.Fatalf("%s: answer not detected in %q", v, answered)
		}
		if IsAwaiting(OwnerHandle, answered, "") {
			t.Fatalf("%s: answered gate still reads as awaiting", v)
		}
	}
}

// MarkerAnswered must not be a substring of MarkerAwaiting in either
// direction, or one state would silently satisfy the other's test.
func TestMarkersDoNotMatchEachOther(t *testing.T) {
	if strings.Contains(MarkerAwaiting, MarkerAnswered) || strings.Contains(MarkerAnswered, MarkerAwaiting) {
		t.Fatalf("markers overlap: %q / %q", MarkerAwaiting, MarkerAnswered)
	}
	if IsAnswered(MarkerAwaiting, "") {
		t.Fatalf("the awaiting marker reads as answered")
	}
}

// Assigning work to the human IS the claim that the ball is in their court —
// including the by-hand assignment the overseer reached for before this
// product existed (🎯T383's row). A reader that only honoured the structured
// marker would still spawn against those.
func TestBareOwnerAssignmentIsAwaiting(t *testing.T) {
	for _, owner := range []string{"owner", "Owner", "@owner", " marcelo ", "the owner"} {
		if !IsOwnerHandle(owner) {
			t.Fatalf("%q not recognised as the owner", owner)
		}
		if !IsAwaiting(owner, "assigned by hand, see context", "") {
			t.Fatalf("%q assignment does not read as awaiting", owner)
		}
	}
	for _, other := range []string{"", "jevons-po", "bullseye-po", "jv-t383-auto"} {
		if IsOwnerHandle(other) {
			t.Fatalf("%q must not read as the owner", other)
		}
	}
	// Another driver is not an owner gate: someone else has it, which is a
	// different claim with a different park reason.
	if IsAwaiting("bullseye-po", "driving this from the bullseye side", "") {
		t.Fatal("assignment to another agent must not read as an owner gate")
	}
}

func TestParseVerdict(t *testing.T) {
	for _, s := range []string{"accept", "Accepted", " yes ", "approve"} {
		if v, err := ParseVerdict(s); err != nil || v != VerdictAccept {
			t.Fatalf("ParseVerdict(%q) = %q, %v", s, v, err)
		}
	}
	for _, s := range []string{"reject", "No", "denied"} {
		if v, err := ParseVerdict(s); err != nil || v != VerdictReject {
			t.Fatalf("ParseVerdict(%q) = %q, %v", s, v, err)
		}
	}
	if _, err := ParseVerdict("maybe"); err == nil {
		t.Fatal("ParseVerdict accepted a non-verdict")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownergate

import (
	"strings"
	"testing"
	"time"
)

// t728Attestation is the shape that matters: a real attestation, several
// paragraphs long. 🎯T711's is 2.6 KB across three of them. Any recovery that
// stops at a blank line truncates it while still looking non-empty.
const t728Attestation = `Achieved by jv-t711-po-interrupt on the hermetic clauses.

ORACLE: GATE exit=0 GREEN id=45acfeb7, tree=clean@6e9da8f5a096, covering TestT711
plus the whole deliver/queue family.

RESIDUAL, owner-only class 3: the owner has not confirmed the live ge-po
specimen behaves as intended under a doctrine direct.`

func t728Reopen() Reopen {
	return Reopen{
		Record: Record{
			Question:   "Does the live ge-po seat preempt an in-flight turn as intended?",
			Evidence:   "landed at 6e9da8f5; GATE id=45acfeb7 GREEN over TestT711",
			RecordedBy: "jevons-po",
			Now:        time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
		},
		AchievedOn:  "2026-09-21",
		Attestation: t728Attestation,
	}
}

// The deliverable assertion, at the unit level: both texts the ceremony writes
// carry the attestation back byte for byte, blank lines and all.
func TestT728ReopenCarriesAttestationByteForByte(t *testing.T) {
	r := t728Reopen()
	reason, err := r.Reason()
	if err != nil {
		t.Fatal(err)
	}
	gate, err := r.GateReason()
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"reopen reason": reason, "gate reason": gate} {
		got := PreservedAttestation(text)
		if got != t728Attestation {
			t.Errorf("%s did not carry the attestation intact.\n got %q\nwant %q", name, got, t728Attestation)
		}
	}
}

// Why the carry exists at all, rather than reading bullseye's own audit back:
// that read stops at the first blank line. This test is the trap, pinned.
func TestT728AchieveAuditTruncatesWhereTheCarryDoesNot(t *testing.T) {
	ctx := "Reopen/file as regression sibling.\n\nAchieved 2026-09-21: " + t728Attestation
	audit := AttestationFromAchieveAudit(ctx)
	if audit == t728Attestation {
		t.Fatal("audit recovery no longer truncates — if bullseye's context round trip now preserves " +
			"paragraphs, say so in the doc comment; do not rely on it silently")
	}
	if !strings.HasPrefix(t728Attestation, audit) || audit == "" {
		t.Fatalf("audit recovery returned something other than a prefix: %q", audit)
	}
	gate, err := t728Reopen().GateReason()
	if err != nil {
		t.Fatal(err)
	}
	if PreservedAttestation(gate) != t728Attestation {
		t.Fatal("the carried copy must not truncate where the audit does")
	}
}

func TestT728ReopenRefusesWithoutEvidence(t *testing.T) {
	r := t728Reopen()
	r.Record.Evidence = "done"
	if _, err := r.Reason(); err == nil {
		t.Fatal("reopen reason accepted an unevidenced claim: reopening on that would un-achieve work on prose")
	}
	if _, err := r.GateReason(); err == nil {
		t.Fatal("gate reason accepted an unevidenced claim")
	}
}

func TestT728ReopenedGateIsRecognisedAndDated(t *testing.T) {
	gate, err := t728Reopen().GateReason()
	if err != nil {
		t.Fatal(err)
	}
	if !IsReopenedGate(gate) {
		t.Fatalf("a reopened gate is not recognised, so accept would never re-achieve: %q", gate)
	}
	if got := ReopenedAchievedDate(gate); got != "2026-09-21" {
		t.Fatalf("achieved date = %q, want 2026-09-21", got)
	}
	plain, err := Record{Question: "Looks right?", Evidence: "landed at abcdef1; GATE id=deadbeef"}.Reason()
	if err != nil {
		t.Fatal(err)
	}
	if IsReopenedGate(plain) {
		t.Fatal("an ordinary gate must not be mistaken for a reopened one — accept would re-achieve a row that was never achieved")
	}
}

// An unknown date is said, not invented (🎯T31).
func TestT728MissingAchievedDateIsNamedNotInvented(t *testing.T) {
	r := t728Reopen()
	r.AchievedOn = ""
	gate, err := r.GateReason()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gate, "unrecorded date") {
		t.Fatalf("missing achieved date not named: %q", gate)
	}
	if d := ReopenedAchievedDate(gate); d != "" {
		t.Fatalf("invented an achieved date %q", d)
	}
}

func TestT728RestoreAttestationReproducesTheOriginal(t *testing.T) {
	att := RestoreAttestation(VerdictAccept, "looks right", "jevons-po", "2026-09-21",
		t728Attestation, time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC))
	if got := PreservedAttestation(att); got != t728Attestation {
		t.Errorf("restored attestation lost the original.\n got %q\nwant %q", got, t728Attestation)
	}
	if !strings.Contains(att, MarkerAnswered) || !strings.Contains(att, "2026-09-21") {
		t.Errorf("restored attestation drops the verdict or the original date: %q", att)
	}
	missing := RestoreAttestation(VerdictAccept, "", "jevons-po", "2026-09-21", "", time.Now())
	if !strings.Contains(missing, "could not be recovered") {
		t.Errorf("an unrecoverable attestation must say so, not re-achieve quietly: %q", missing)
	}
}

// A re-achieved row must not read as still awaiting the owner.
func TestT728RestoredAttestationIsNotAwaiting(t *testing.T) {
	att := RestoreAttestation(VerdictAccept, "", "jevons-po", "2026-09-21", t728Attestation, time.Now())
	if !IsAnswered(att, "") {
		t.Fatal("restored attestation does not read as answered")
	}
	if IsAwaiting("", att, "") {
		t.Fatal("restored attestation still reads as awaiting the owner")
	}
}

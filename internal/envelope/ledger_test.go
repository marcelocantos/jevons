// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"
	"testing"
)

func TestSilentLedgerRankedRoundTrip(t *testing.T) {
	m := &Message{
		Kind:         KindFinishReport,
		Target:       "T536.1",
		SHA:          "abcdef0123456",
		GateID:       "9f13c0a2",
		Verdict:      VerdictGreen,
		SilentLedger: SilentLedgerRanked,
		Decisions: []SilentDecision{
			{Confidence: 0.2, Choice: "optimistic concurrency", Why: "spec silent on locking"},
			{Confidence: 0.5, Choice: "shared-sqlite-table", Why: "no isolation guidance"},
		},
		Payload: "Work landed.",
	}
	raw := Format(m)
	if !strings.Contains(raw, "jevons: silent-ledger ranked") {
		t.Fatalf("missing ledger slot:\n%s", raw)
	}
	if !strings.Contains(raw, "silent-decision confidence=0.2") {
		t.Fatalf("missing first decision:\n%s", raw)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, raw)
	}
	if got.SilentLedger != SilentLedgerRanked {
		t.Fatalf("state=%v", got.SilentLedger)
	}
	ds := got.SilentDecisions()
	if len(ds) != 2 {
		t.Fatalf("decisions=%v", ds)
	}
	if ds[0].Choice != "optimistic concurrency" || ds[0].Confidence != 0.2 {
		t.Fatalf("first=%+v", ds[0])
	}
	if ds[1].Choice != "shared-sqlite-table" {
		t.Fatalf("second=%+v", ds[1])
	}
}

func TestSilentLedgerExplicitEmptyAccepted(t *testing.T) {
	raw := Format(&Message{
		Kind:         KindFinishReport,
		Target:       "T536.1",
		SHA:          "abcdef0123456",
		SilentLedger: SilentLedgerEmpty,
		Payload:      "No silent decisions.",
	})
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.SilentLedger != SilentLedgerEmpty || !got.HasSilentLedger() {
		t.Fatalf("state=%v", got.SilentLedger)
	}
	if got.SilentDecisions() != nil {
		t.Fatal("empty ledger has no decisions")
	}
}

func TestSilentLedgerMissingOnFinishReportFlagged(t *testing.T) {
	raw := "```jevons\njevons: kind finish-report\njevons: target T536.1\njevons: oracle sha=abcdef0123456\njevons: verdict GREEN\n```\n\nDone."
	m, err := Parse(raw)
	if m == nil {
		t.Fatal("malformed still returns message")
	}
	if err == nil {
		t.Fatal("missing silent-ledger must be flagged")
	}
	if !strings.Contains(err.Error(), "silent-ledger") {
		t.Fatalf("err=%v", err)
	}
	if !MissingSilentLedger(m) {
		t.Fatal("MissingSilentLedger")
	}
}

func TestSilentLedgerOutOfOrderFlagged(t *testing.T) {
	raw := "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T536.1\n" +
		"jevons: oracle sha=abcdef0123456\n" +
		"jevons: silent-ledger ranked\n" +
		"jevons: silent-decision confidence=0.8 choice=high\n" +
		"jevons: silent-decision confidence=0.2 choice=low\n" +
		"```\n\nDone."
	_, err := Parse(raw)
	if err == nil || !strings.Contains(err.Error(), "least-confident") {
		t.Fatalf("want least-confident-first error, got %v", err)
	}
}

func TestReadSilentLedgerGateHelper(t *testing.T) {
	raw := Format(&Message{
		Kind:         KindFinishReport,
		Target:       "T536.1",
		SHA:          "abcdef0123456",
		SilentLedger: SilentLedgerRanked,
		Decisions: []SilentDecision{
			{Confidence: 0.3, Choice: "retry-2s", Why: "brief silent on backoff"},
		},
		Payload: "ok",
	})
	st, ds, ok := ReadSilentLedger(raw)
	if !ok || st != SilentLedgerRanked || len(ds) != 1 {
		t.Fatalf("st=%v ds=%v ok=%v", st, ds, ok)
	}
	if ds[0].Choice != "retry-2s" {
		t.Fatalf("choice=%q", ds[0].Choice)
	}
	_, _, ok = ReadSilentLedger("unenveloped prose")
	if ok {
		t.Fatal("unenveloped is not ok")
	}
}

// 🎯T1115: an unquoted why= value containing multiple space-separated,
// hyphenated and parenthetical words must parse as one free-text field,
// not be re-tokenized word-by-word into spurious "not key=value" errors.
func TestSilentDecisionWhyFreeTextMultiWord(t *testing.T) {
	raw := "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T1115\n" +
		"jevons: oracle sha=abcdef0123456\n" +
		"jevons: silent-ledger ranked\n" +
		"jevons: silent-decision confidence=0.5 choice=exclude-only-when-seatstate-observed-dead why=a never-observed seat (proc nil, no seatstate authority) keeps the old conservative behaviour\n" +
		"```\n"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(m.Decisions) != 1 {
		t.Fatalf("want 1 decision, got %d: %+v", len(m.Decisions), m.Decisions)
	}
	got := m.Decisions[0]
	if got.Choice != "exclude-only-when-seatstate-observed-dead" {
		t.Fatalf("choice = %q", got.Choice)
	}
	want := "a never-observed seat (proc nil, no seatstate authority) keeps the old conservative behaviour"
	if got.Why != want {
		t.Fatalf("why = %q, want %q", got.Why, want)
	}
}

// 🎯T1115: direct unit coverage of parseSilentDecision for the same shape,
// isolated from the fenced-envelope line splitter.
func TestParseSilentDecisionWhyFreeText(t *testing.T) {
	raw := `confidence=0.5 choice=exclude-only-when-seatstate-observed-dead why=a never-observed seat (proc nil, no seatstate authority) keeps the old conservative behaviour`
	d, err := parseSilentDecision(raw)
	if err != nil {
		t.Fatalf("parseSilentDecision: %v", err)
	}
	want := "a never-observed seat (proc nil, no seatstate authority) keeps the old conservative behaviour"
	if d.Why != want {
		t.Fatalf("why = %q, want %q", d.Why, want)
	}
	if d.Choice != "exclude-only-when-seatstate-observed-dead" {
		t.Fatalf("choice = %q", d.Choice)
	}
	if d.Confidence != 0.5 {
		t.Fatalf("confidence = %v", d.Confidence)
	}
}

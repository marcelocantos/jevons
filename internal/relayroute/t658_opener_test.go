// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package relayroute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// t658Fixture reads one of the 2026-09-15 incident texts verbatim. Each is a
// message a worker actually sent, or a turn it actually painted, that the
// relay routed past the PO as an oracle_done finish (🎯T658).
func t658Fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "t658", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 🎯T658 acceptance 1: a progress opener with no finish-report envelope, no
// GATE line and no SHA is progress — parent — not oracle_done. The table is
// the incident: the jv-t657-steer-ui turn-3 opener, the jv-t658 seat's
// "Proceeding…" line, and the two scout-report sends that were actually
// rerouted (the jv-t657 one is a valid envelope whose prose says "Scout
// done" and whose fog lines say "oracle"; the jv-t658 one fails slot
// validation on one silent-decision token).
func TestT658ProgressOpenersStayOnParent(t *testing.T) {
	for _, name := range []string{
		"t657-opener.txt",
		"t658-proceeding.txt",
		"t657-scout-report.txt",
		"t658-scout-report-malformed.txt",
	} {
		text := t658Fixture(t, name)
		if got := Classify(text); got != RouteParent {
			t.Errorf("%s: Classify=%s want parent", name, got)
		}
		if got := Reason(text); got != "parent" {
			t.Errorf("%s: Reason=%s want parent", name, got)
		}
	}
}

// The jv-t658 fixture is the malformed-envelope shape on its own: Parse
// returns a message AND an error, and the old classifier scanned the whole
// text — fence slots included — for finish words. Pin that the fixture still
// has that shape, so the table above keeps testing what it claims to.
func TestT658MalformedEnvelopeFixtureShape(t *testing.T) {
	text := t658Fixture(t, "t658-scout-report-malformed.txt")
	m, err := envelope.Parse(text)
	if m == nil || err == nil {
		t.Fatalf("fixture must parse as an envelope with a validation error; m=%v err=%v", m != nil, err)
	}
	if m.Kind != envelope.KindScoutReport {
		t.Fatalf("kind=%q want scout-report", m.Kind)
	}
	lower := strings.ToLower(text)
	if !oracleDone(lower) {
		t.Fatal("fixture must trip the keyword scan when read whole — otherwise it no longer reproduces the incident")
	}
}

// A malformed finish-report is "cannot tell", which the package contract
// routes to the parent; a valid oracle-backed finish-report still skips the
// hop. The fix must not turn T392.7 off.
func TestT658MalformedFinishReportStaysOnParentValidOneSkips(t *testing.T) {
	valid := "```jevons\njevons: kind finish-report\njevons: target T658\njevons: oracle sha=abcdef0123456\njevons: verdict GREEN\njevons: silent-ledger none\n```\n\nWork landed."
	if got := Classify(valid); got != RouteOverseer {
		t.Fatalf("valid oracle finish-report: Classify=%s want overseer", got)
	}
	if got := Reason(valid); got != "oracle_done" {
		t.Fatalf("valid oracle finish-report: Reason=%s want oracle_done", got)
	}
	malformed := "```jevons\njevons: kind finish-report\njevons: target T658\njevons: oracle sha=abcdef0123456\njevons: verdict GREEN\njevons: silent-ledger ranked\njevons: silent-decision confidence=0.5 choice=x why=this token is not key=value\n```\n\nDone. GATE x GREEN. SHA abcdef0. Tests pass."
	if m, err := envelope.Parse(malformed); m == nil || err == nil {
		t.Fatalf("fixture must be a malformed envelope; m=%v err=%v", m != nil, err)
	}
	if got := Classify(malformed); got != RouteParent {
		t.Fatalf("malformed finish-report: Classify=%s want parent", got)
	}
	if got := Reason(malformed); got != "parent" {
		t.Fatalf("malformed finish-report: Reason=%s want parent", got)
	}
}

// An envelope behind a prefix this package does not know is not an
// envelope to Parse, and the prefix is not the sender's report either.
// On 2026-09-15 that prefix was the product-owner doctrine ("oracle",
// "done") from a first-send wrap; the classifier read it as the finish.
func TestT658FenceBehindUnknownPrefixStaysOnParent(t *testing.T) {
	doctrine := "[Some daemon framing]\n\nRefuse bare done without oracle evidence (GATE … GREEN).\n\n"
	text := doctrine + t658Fixture(t, "t657-scout-report.txt")
	if m, _ := envelope.Parse(text); m != nil {
		t.Fatal("fixture must not parse as a line-1 envelope")
	}
	if got := Classify(text); got != RouteParent {
		t.Fatalf("Classify=%s want parent", got)
	}
	if got := Reason(text); got != "parent" {
		t.Fatalf("Reason=%s want parent", got)
	}
	// Control: the same prefix with no fence behind it is still scanned as
	// prose, so the guard is about the fence, not about the words.
	if got := Classify(doctrine + "Done. GATE x GREEN, tests pass."); got != RouteOverseer {
		t.Fatalf("control: Classify=%s want overseer", got)
	}
}

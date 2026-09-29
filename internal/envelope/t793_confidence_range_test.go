// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"
	"testing"
)

// TestT793ConfidenceOutOfRangeIsNamedAndActionable is the hermetic oracle
// for 🎯T793: a finish-report whose only defect is a silent-decision
// confidence written as a 1-10 rank (e.g. 4) produces a named, actionable
// parse error, and CorrectionNotice turns that into a message the AUTHOR
// (not just the parent) can act on to resend a corrected report.
func TestT793ConfidenceOutOfRangeIsNamedAndActionable(t *testing.T) {
	raw := "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T793\n" +
		"jevons: oracle sha=abcdef0123456\n" +
		"jevons: verdict GREEN\n" +
		"jevons: silent-ledger ranked\n" +
		"jevons: silent-decision confidence=4 choice=\"used a rank not a probability\"\n" +
		"```\n\nDone."

	m, err := Parse(raw)
	if err == nil {
		t.Fatal("want malformed: confidence=4 is out of [0,1]")
	}
	if !strings.Contains(err.Error(), "confidence") || !strings.Contains(err.Error(), "[0,1]") {
		t.Fatalf("error does not name the field/range: %v", err)
	}
	if m == nil || !m.Kind.LoadBearing() {
		t.Fatalf("want a load-bearing kind even when malformed, got %v", m)
	}

	notice := CorrectionNotice(raw)
	if notice == "" {
		t.Fatal("want a correction notice for a malformed load-bearing envelope")
	}
	for _, want := range []string{"confidence", "0 to 1", "resend", "finish-report"} {
		if !strings.Contains(strings.ToLower(notice), strings.ToLower(want)) {
			t.Errorf("correction notice missing %q:\n%s", want, notice)
		}
	}

	// A corrected resend parses clean.
	fixed := strings.Replace(raw, "confidence=4", "confidence=0.4", 1)
	if _, err := Parse(fixed); err != nil {
		t.Fatalf("corrected resend still malformed: %v", err)
	}
	if got := CorrectionNotice(fixed); got != "" {
		t.Fatalf("corrected resend should not need a notice, got %q", got)
	}
}

// TestT793CorrectionNoticeEmptyWhenClean covers the non-malformed and
// non-load-bearing cases so CorrectionNotice does not fire noise.
func TestT793CorrectionNoticeEmptyWhenClean(t *testing.T) {
	if got := CorrectionNotice("just prose, no envelope"); got != "" {
		t.Fatalf("prose should not get a notice, got %q", got)
	}
	clean := Format(&Message{
		Kind:         KindFinishReport,
		Target:       "T793",
		SHA:          "abcdef0123456",
		Verdict:      VerdictGreen,
		SilentLedger: SilentLedgerEmpty,
		Payload:      "Done.",
	})
	if got := CorrectionNotice(clean); got != "" {
		t.Fatalf("valid envelope should not get a notice, got %q", got)
	}
}

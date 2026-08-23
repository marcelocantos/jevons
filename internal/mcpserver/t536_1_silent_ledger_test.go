// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

func TestT5361RankedSilentLedgerIsOracleEvidence(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T536.1",
		SHA:          "abcdef0123456",
		GateID:       "9f13c0a2",
		Verdict:      envelope.VerdictGreen,
		SilentLedger: envelope.SilentLedgerRanked,
		Decisions: []envelope.SilentDecision{
			{Confidence: 0.2, Choice: "optimistic concurrency", Why: "spec silent on locking"},
			{Confidence: 0.5, Choice: "shared-sqlite-table", Why: "no isolation guidance"},
		},
		Payload: "Work landed.",
	})
	if ClassifyCompletionReport(raw) != CompletionOracleEvidence {
		t.Fatalf("class=%s", ClassifyCompletionReport(raw))
	}
	st, ds, ok := envelope.ReadSilentLedger(raw)
	if !ok || st != envelope.SilentLedgerRanked || len(ds) != 2 {
		t.Fatalf("gate helper st=%v ds=%v ok=%v", st, ds, ok)
	}
}

func TestT5361ExplicitEmptySilentLedgerAccepted(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T536.1",
		SHA:          "abcdef0123456",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "No silent decisions.",
	})
	if ClassifyCompletionReport(raw) != CompletionOracleEvidence {
		t.Fatalf("class=%s", ClassifyCompletionReport(raw))
	}
	if LooksLikeMissingSilentLedger(raw) {
		t.Fatal("explicit none must not be missing")
	}
}

func TestT5361MissingSilentLedgerFlaggedNotComplete(t *testing.T) {
	raw := "```jevons\njevons: kind finish-report\njevons: target T536.1\njevons: oracle sha=abcdef0123456\njevons: verdict GREEN\n```\n\ngo test PASS"
	if ClassifyCompletionReport(raw) != CompletionMissingSilentLedger {
		t.Fatalf("class=%s want missing_silent_ledger", ClassifyCompletionReport(raw))
	}
	if !LooksLikeMissingSilentLedger(raw) {
		t.Fatal("LooksLikeMissingSilentLedger")
	}
	if HasOracleOrRisk(raw) {
		t.Fatal("missing ledger must not count as complete oracle/risk")
	}
	s := &Server{}
	out, drop := s.applyEnvelopeControls("jv-t536.1-silent-ledger", raw)
	if drop {
		t.Fatal("flagged finish-report must still deliver")
	}
	if !strings.Contains(out, envelope.BannerHeading) || !strings.Contains(out, "silent-ledger") {
		t.Fatalf("want silent-ledger banner:\n%s", out)
	}
}

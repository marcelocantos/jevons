// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T581: a mid-mission status whose completion word is a local clause
// ("reading done") is never reaped as finished work. Fixture: the exact
// jv-t564-no-ctx-ceiling report 20260829T011241Z-50e24dab, which reap_done
// read as finished_work (claim_marker=done, report_span="Status: reading
// done, no tool hung;").

func loadT581Fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "t581_jv_t564_reading_done_report.md"))
	if err != nil {
		t.Fatalf("read T581 fixture: %v", err)
	}
	return string(b)
}

func TestT581IncidentReportIsTheIncidentShape(t *testing.T) {
	lower := asciiLower(loadT581Fixture(t))
	if !strings.Contains(lower, "reading done") || !strings.Contains(lower, "implementing now") {
		t.Fatal("fixture lost the local done-word grammar")
	}
	if !hasCompletionClaim(lower) || !hasOracleEvidence(lower) {
		t.Fatal("fixture no longer carries the done + oracle words that reaped it")
	}
	if hasForwardLookingPlan(lower) {
		t.Fatal("T577's list now covers this report; T581 rule would be untested")
	}
}

func TestT581IncidentReportIsNotTerminal(t *testing.T) {
	report := loadT581Fixture(t)
	if LooksLikeFinishedWorkReport(report) {
		t.Fatal("jv-t564 reading-done status classified as finished_work")
	}
	reg := t395Registry(t, "jv-t564-no-ctx-ceiling")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t564-no-ctx-ceiling", report, nil)
	if ok {
		t.Fatalf("incident report reaps (reason %s)", reason)
	}
}

func TestT581IncidentSeatRetainedThroughSink(t *testing.T) {
	const agent = "jv-t564-no-ctx-ceiling"
	s, reg := t471SinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{Type: "assistant", Text: loadT581Fixture(t), StopReason: "end_turn"})
	if reg.Def(agent) == nil {
		t.Fatal("reading-done status deregistered the seat")
	}
}

func TestT581FinishReportEnvelopeStillReaps(t *testing.T) {
	raw := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T581",
		SHA:          "abcdef0123456",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "Reading done; implementing now was the last checkpoint. All landed.",
	})
	reg := t395Registry(t, "jv-t581-envelope")
	if ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t581-envelope", raw, nil); !ok {
		t.Fatalf("finish-report envelope did not reap (reason %s)", reason)
	}
}

func TestT581LocalDoneShapes(t *testing.T) {
	cases := []struct {
		report string
		local  bool
	}{
		{"Status: reading done, no tool hung; implementing now — governor wiring.", true},
		{"Recon done. Now wiring the loader; tests after.", true},
		{"Survey done; next: the writer. Commit abcdef1 has the scaffold.", true},
		{"Done. Commit abcdef1, go test ./... green.", false},
		{"Reading done; implementing now. Done: commit abcdef1, go test green.", false},
		{"Everything is done now.", false},
		{"Mission complete.", false},
	}
	for _, tc := range cases {
		lower := strings.ToLower(tc.report)
		if got := allCompletionClaimsLocal(lower); got != tc.local {
			t.Errorf("allCompletionClaimsLocal(%q) = %v, want %v", tc.report, got, tc.local)
		}
		if tc.local && hasFinishShape(lower) {
			t.Errorf("hasFinishShape(%q) = true for a local done", tc.report)
		}
	}
}

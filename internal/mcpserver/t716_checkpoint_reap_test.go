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

// 🎯T716: a report that names its own next turn is not terminal, and never
// reaps the seat.
//
// Fixture: jv-t702-seat-activity stored report 20260920T144827Z-6860fdbc
// (1588 bytes). It opens "Checkpoint 3 — code landed in the working tree,
// builds green, not yet committed or tested" and closes with a "**Next
// turn:**" remaining-work list. product:reap_done removed the seat as
// finished_work. The phrase "code landed" meant landed in the working tree,
// explicitly contrasted with committed.
//
// Why the older keep-paths missed it:
//   - T497's checkpoint declaration requires a punctuation break immediately
//     after the word. "Checkpoint 3 —" has a number in between, so it was
//     a mention, not a declaration.
//   - T577's remaining-work list has "next step" but not "next turn:".
//   - T470's explicit-incomplete list has "no commit yet" / "not yet done"
//     but not "not yet committed" / "not yet tested".
//   - T536.3 already keeps a typed scout-report; this report had no envelope.
//
// A mutation that restores the reap on this checkpoint text goes RED.

func loadT716Fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "t716_jv_t702_checkpoint_report.md"))
	if err != nil {
		t.Fatalf("read T716 fixture: %v", err)
	}
	if len(b) < 1500 {
		t.Fatalf("fixture too short to be the incident report: %d bytes", len(b))
	}
	return string(b)
}

func TestT716IncidentReportIsTheIncidentShape(t *testing.T) {
	report := loadT716Fixture(t)
	lower := asciiLower(report)
	if !strings.HasPrefix(strings.TrimSpace(lower), "checkpoint 3") {
		t.Fatal("fixture lost its numbered checkpoint opening")
	}
	if !strings.Contains(lower, "not yet committed or tested") {
		t.Fatal("fixture lost its not-yet-committed/tested clause")
	}
	if !strings.Contains(lower, "next turn:") {
		t.Fatal("fixture lost its Next turn: remaining-work heading")
	}
	if !hasCompletionClaim(lower) {
		t.Fatal("fixture no longer carries a completion word — cannot reproduce the incident")
	}
	if !hasOracleEvidence(lower) {
		t.Fatal("fixture no longer carries oracle-shaped words — cannot reproduce the incident")
	}
}

func TestT716IncidentReportIsNotTerminal(t *testing.T) {
	report := loadT716Fixture(t)
	if LooksLikeFinishedWorkReport(report) {
		t.Fatal("jv-t702 checkpoint 3 classified as finished_work")
	}
	if ask := ClassifyReportAsk(report); ask != AskCheckpoint {
		t.Fatalf("ClassifyReportAsk = %s, want checkpoint", ask)
	}
	reg := t395Registry(t, "jv-t702-seat-activity")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t702-seat-activity", report, nil)
	if ok {
		t.Fatalf("incident report reaps (reason %s)", reason)
	}
}

func TestT716IncidentSeatRetainedThroughSink(t *testing.T) {
	report := loadT716Fixture(t)
	const agent = "jv-t702-seat-activity"
	s, reg := t471SinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{
		Type:       "assistant",
		Text:       report,
		StopReason: "end_turn",
	})
	if reg.Def(agent) == nil {
		t.Fatal("checkpoint 3 report deregistered the seat")
	}
}

func TestT716KeepSignalsIndependently(t *testing.T) {
	// Each signal must keep on its own, so a regression in one path
	// cannot hide behind another. Every case carries the done+green
	// words that reaped the incident.
	cases := []struct {
		name   string
		report string
		want   ReportAskClass
	}{
		{
			name:   "numbered checkpoint declaration",
			report: "Checkpoint 3 — mapped the parser. Builds green. Done this turn: the map.",
			want:   AskCheckpoint,
		},
		{
			name:   "not yet committed",
			report: "Done this turn. Builds green. Not yet committed or tested.",
			want:   AskExplicitIncomplete,
		},
		{
			name:   "next turn remaining work",
			report: "Done this turn. Builds green.\n\n**Next turn:** write the tests and commit.",
			want:   AskExplicitIncomplete,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyReportAsk(tc.report); got != tc.want {
				t.Fatalf("ClassifyReportAsk = %s, want %s", got, tc.want)
			}
			if LooksLikeFinishedWorkReport(tc.report) {
				t.Fatal("keep-signal classified as finished work")
			}
			reg := t395Registry(t, "jv-t716-keep")
			ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t716-keep", tc.report, nil)
			if ok {
				t.Fatalf("reaped (%s)", reason)
			}
		})
	}
}

func TestT716NumberedCheckpointIsADeclaration(t *testing.T) {
	cases := []struct {
		name   string
		report string
		want   ReportAskClass
	}{
		{"em dash after number", "Checkpoint 3 — parser mapped; writer next.", AskCheckpoint},
		{"colon after number", "Checkpoint 3: wired the decoder.", AskCheckpoint},
		{"hash ordinal", "Checkpoint #3 — ending this slice.", AskCheckpoint},
		{"mention still a mention", "Done. Commit 4f2b8c1; go test PASS. Checkpoint reports now classify as open work.", AskNone},
		{"handling still a mention", "Done. Commit 4f2b8c1; go test PASS. Checkpoint handling no longer reaps numbered banners.", AskNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyReportAsk(tc.report); got != tc.want {
				t.Fatalf("ClassifyReportAsk = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestT716EnvelopedKindWinsOverProse(t *testing.T) {
	payload := loadT716Fixture(t)
	ping := envelope.Format(&envelope.Message{
		Kind:    envelope.KindStatusPing,
		Target:  "T716",
		Status:  envelope.ProgressInProgress,
		Payload: payload,
	})
	if LooksLikeFinishedWorkReport(ping) {
		t.Fatal("status-ping wrapping checkpoint 3 must not be terminal")
	}
	reg := t395Registry(t, "jv-t716-ping")
	if ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t716-ping", ping, nil); ok {
		t.Fatalf("status-ping reaped (%s)", reason)
	}

	finish := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T716",
		SHA:          "abcdef0123456",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      payload,
	})
	if !LooksLikeFinishedWorkReport(finish) {
		t.Fatal("typed finish-report is terminal even when payload is a checkpoint")
	}
	reg = t395Registry(t, "jv-t716-finish")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t716-finish", finish, nil)
	if !ok {
		t.Fatalf("finish-report envelope did not reap (reason %s)", reason)
	}
}

func TestT716GenuineFinishStillReaps(t *testing.T) {
	report := "🎯T716 done. Landed as commit abcdef0123456 (ancestor of HEAD verified).\n" +
		"go test ./internal/mcpserver -run T716 PASS\n" +
		"Hermetic oracle covers numbered checkpoints both ways."
	if ClassifyReportAsk(report) != AskNone {
		t.Fatalf("genuine finish classified as ask: %s", ClassifyReportAsk(report))
	}
	if !LooksLikeFinishedWorkReport(report) {
		t.Fatal("genuine finish must still reap")
	}
	reg := t395Registry(t, "jv-t716-done")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t716-done", report, nil)
	if !ok {
		t.Fatalf("genuine finish did not reap (reason %s)", reason)
	}
}

func TestT716BareNextTurnWithoutColonStillReaps(t *testing.T) {
	// 🎯T471 control: "Done. Ready for the next turn." is not a remaining-work
	// heading. The colon is what names what the worker will do next turn.
	if !LooksLikeFinishedWorkReport(t471AmbiguousAfterCeiling) {
		t.Fatal("T471 control fixture must still look like finished work")
	}
}

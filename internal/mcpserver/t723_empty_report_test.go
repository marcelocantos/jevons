// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/noopwedge"
)

// 🎯T723: an empty report is not a finished-work claim and never reaps the seat.
//
// T716 kept a report that said too much (named its own next turn). This is
// the same rule from the other end: a report that says nothing. Both collapse
// into one classifier — a reap needs an affirmative claim; everything else
// keeps the seat. AskNoClaim is that keep-class for silence.
//
// Specimen: ge-t190-desktop-cook stored report 20260920T151214Z-85caba2f
// (22 bytes, "No response requested."). The same 22-byte body is
// jv-t706-reaped-alert 20260920T145135Z-85caba2f and jv-t711-po-interrupt
// 20260920T150119Z-85caba2f. The string is a harness artefact of a turn that
// ended without output; it asserts nothing about the mission.
//
// A mutation that reaps on the empty body, or on the literal 22-byte ack,
// goes RED.

// t723HarnessAck is the exact stored text of the three specimen reports
// (22 bytes including the period).
const t723HarnessAck = "No response requested."

func TestT723HarnessAckIsTheIncidentShape(t *testing.T) {
	if len(t723HarnessAck) != 22 {
		t.Fatalf("harness ack is %d bytes, want the 22-byte specimen", len(t723HarnessAck))
	}
	if !noopwedge.IsBareAck(t723HarnessAck) {
		t.Fatal("specimen is no longer T402's exact-match ack — cannot reproduce the incident")
	}
	if hasCompletionClaim(asciiLower(t723HarnessAck)) {
		t.Fatal("specimen grew a completion word — it is no longer a no-claim report")
	}
}

func TestT723NoClaimReportsAreNotTerminal(t *testing.T) {
	cases := []struct {
		name   string
		report string
		marker string
	}{
		{name: "empty body", report: "", marker: "empty"},
		{name: "whitespace only", report: " \n\t ", marker: "empty"},
		{name: "harness ack", report: t723HarnessAck, marker: "bare_ack"},
		{name: "ack without period", report: "No response requested", marker: "bare_ack"},
		{name: "acknowledged", report: "Acknowledged.", marker: "bare_ack"},
		{name: "nothing to report", report: "Nothing to report.", marker: "bare_ack"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := ClassifyReportAskDetail(tc.report)
			if d.Class != AskNoClaim {
				t.Fatalf("ClassifyReportAsk = %s (marker %q), want no_claim", d.Class, d.Marker)
			}
			if d.Marker != tc.marker {
				t.Fatalf("marker = %q, want %q", d.Marker, tc.marker)
			}
			if LooksLikeFinishedWorkReport(tc.report) {
				t.Fatal("no-claim report classified as finished_work")
			}
			if ReportAwaitsOverseer(tc.report) {
				t.Fatal("silence must not count as an overseer ask")
			}
			reg := t395Registry(t, "ge-t190-desktop-cook")
			ok, reason := ShouldAutoReapDoneWorkAgent(reg, "ge-t190-desktop-cook", tc.report, nil)
			if ok {
				t.Fatalf("no-claim report reaps (reason %s)", reason)
			}
			if reason != "no_work_claim" {
				t.Fatalf("reason = %q, want no_work_claim", reason)
			}
		})
	}
}

func TestT723HarnessAckSeatRetainedThroughSink(t *testing.T) {
	const agent = "ge-t190-desktop-cook"
	s, reg := reapSinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{
		Type:       "assistant",
		Text:       t723HarnessAck,
		StopReason: "end_turn",
	})
	if reg.Def(agent) == nil {
		t.Fatal("22-byte harness ack deregistered the seat")
	}
}

func TestT723EmptyTerminalSeatRetainedThroughSink(t *testing.T) {
	const agent = "jv-t723-empty"
	s, reg := reapSinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{
		Type:       "assistant",
		Text:       "",
		StopReason: "end_turn",
	})
	if reg.Def(agent) == nil {
		t.Fatal("empty terminal deregistered the seat")
	}
}

func TestT723AckPlusContentIsNotNoClaim(t *testing.T) {
	// T402 exact-match: a payload after the ack phrase is a report.
	report := "No response requested — but the build is broken, see below."
	if ClassifyReportAsk(report) == AskNoClaim {
		t.Fatal("ack-plus-content classified as no-claim")
	}
}

func TestT723EnvelopedFinishReportStillReaps(t *testing.T) {
	finish := envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T723",
		SHA:          "abcdef0123456",
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "empty-report classifier landed; hermetic oracle green.",
	})
	if !LooksLikeFinishedWorkReport(finish) {
		t.Fatal("typed finish-report is terminal")
	}
	reg := t395Registry(t, "jv-t723-finish")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t723-finish", finish, nil)
	if !ok {
		t.Fatalf("finish-report envelope did not reap (reason %s)", reason)
	}
}

func TestT723GenuineFinishStillReaps(t *testing.T) {
	report := "🎯T723 done. Landed as commit abcdef0123456 (ancestor of HEAD verified).\n" +
		"go test ./internal/mcpserver -run T723 PASS\n" +
		"Hermetic oracle covers empty body and the 22-byte harness ack."
	if ClassifyReportAsk(report) != AskNone {
		t.Fatalf("genuine finish classified as ask: %s", ClassifyReportAsk(report))
	}
	if !LooksLikeFinishedWorkReport(report) {
		t.Fatal("genuine finish must still reap")
	}
	reg := t395Registry(t, "jv-t723-done")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t723-done", report, nil)
	if !ok {
		t.Fatalf("genuine finish did not reap (reason %s)", reason)
	}
}

func TestT723EmptyBodyMutationGoesRed(t *testing.T) {
	// Acceptance: a mutation that reaps on the empty body goes RED.
	// AskNoClaim vetoes even if a later finish-shape change matched silence.
	reg := t395Registry(t, "jv-t723-empty-mut")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t723-empty-mut", "", nil)
	if ok {
		t.Fatalf("empty body reaped (reason %s)", reason)
	}
	if LooksLikeFinishedWorkReport("") {
		t.Fatal("empty body classified as finished_work")
	}
	if ClassifyReportAsk("") != AskNoClaim {
		t.Fatalf("empty body classified %s, want no_claim", ClassifyReportAsk(""))
	}
}

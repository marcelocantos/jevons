// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T829 specimen 1: cl-t119-broker-gate, reaped 2026-09-21T21:10:03Z
// (~/.jevons/logs/events.jsonl line report_offset=1502, claim_marker
// "achieved"). The worker's own gate ("its make gate") was still on the
// last step and later died with no result; the only completion marker in
// the report is inside a sentence relaying what the OWNER already ran on a
// different target (🎯T126), not a claim the worker made about its own
// mission (🎯T119).
const t829BrokerGateReport = `Status: still on the last step of the make gate for my own change; it has not printed a verdict yet.

The owner already ran it at 36a37e0 as GREEN (881ba165), and T126 is achieved on it.

I'm waiting on my own run before reporting either way; the gate died once already with no result, so I'm rerunning it now.`

func TestT829BrokerGateAttributedClaimDoesNotReap(t *testing.T) {
	if LooksLikeFinishedWorkReport(t829BrokerGateReport) {
		marker, span, _, _ := FindCompletionClaim(t829BrokerGateReport)
		t.Fatalf("the cl-t119-broker-gate report read as a finish; it would have been reaped on marker %q in %q", marker, span)
	}
}

// The red witness: without maskAttributedClauses, the unmasked scan reads
// "achieved" (and the GREEN oracle marker) as the worker's own claim.
func TestT829WitnessTheUnmaskedScanMatched(t *testing.T) {
	lower := asciiLower(t829BrokerGateReport)
	if !hasAnyCompletionMarker(lower) {
		t.Fatal("specimen no longer carries a completion marker; the test has drifted off the shape it pins")
	}
	if !hasOracleEvidence(lower) {
		t.Fatal("specimen no longer carries oracle-evidence vocabulary (GREEN); without it hasFinishShape never reached its short-circuit")
	}
	if hasCompletionClaim(lower) {
		t.Fatal("masked scan still reads an asserted claim in a report whose only marker relays the owner's action on a different target")
	}
}

func TestT829SeatIsKeptOnAnAttributedClaim(t *testing.T) {
	const agent = "cl-t119-broker-gate"
	reg := t395Registry(t, agent)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, agent, t829BrokerGateReport, nil)
	if ok {
		t.Fatalf("a report whose only completion marker relays another party's action reaps (reason %s)", reason)
	}
	if reason != "not_finished_work_report" {
		t.Errorf("reason = %q, want not_finished_work_report", reason)
	}

	s, reg2 := reapSinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{
		Type:       "assistant",
		Text:       t829BrokerGateReport,
		StopReason: "end_turn",
	})
	if reg2.Def(agent) == nil {
		t.Fatal("the sink deregistered a seat whose report relayed the owner's claim about a different target")
	}
}

// A report that DOES assert completion in the worker's own voice must still
// reap even when it happens to mention the owner elsewhere — the fix must not
// over-mask every sentence naming "owner".
const t829GenuineFinishReport = `I finished my own change and committed it as 477983f4.

The owner asked for a screenshot too; I have attached one.

GOAL_STATUS: complete`

func TestT829GenuineOwnClaimStillReaps(t *testing.T) {
	if !LooksLikeFinishedWorkReport(t829GenuineFinishReport) {
		t.Fatal("a report that asserts its own completion, and separately mentions the owner without an attribution lead-in, must still reap")
	}
}

// Each attribution shape on its own, so a regression names which lead-in
// stopped masking.
func TestT829AttributionShapes(t *testing.T) {
	cases := []struct {
		name   string
		report string
		finish bool
	}{
		{"owner already ran it",
			"The owner already ran it and confirmed T50 is achieved. My own gate is still running.", false},
		{"parent reported done",
			"The parent reported the build is done on their side. I am still waiting on my own gate.", false},
		{"PO said complete",
			"The PO said the migration is complete upstream. My own slice has not landed.", false},
		{"per the owner",
			"Per the owner, the earlier run is finished. I have not verified that myself and my change is unlanded.", false},

		{"worker's own bare claim still reaps",
			"Done.", true},
		{"worker's own claim with oracle evidence still reaps",
			"My change is committed as 477983f4. Oracle: go test ./internal/mcpserver -run T829, GREEN. The work is done.", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LooksLikeFinishedWorkReport(tc.report)
			if got != tc.finish {
				marker, span, _, _ := FindCompletionClaim(tc.report)
				t.Fatalf("LooksLikeFinishedWorkReport = %v, want %v (marker %q in %q)", got, tc.finish, marker, span)
			}
		})
	}
}

// claimScanText must not move any byte — the same 🎯T750 offset-preservation
// obligation applies to the new mask.
func TestT829MaskPreservesOffsets(t *testing.T) {
	for _, s := range []string{t829BrokerGateReport, t829GenuineFinishReport} {
		if got := len(claimScanText(s)); got != len(s) {
			t.Errorf("claimScanText changed length: got %d, want %d", got, len(s))
		}
	}
}

// 🎯T829 acceptance 1 names jv-t762-dropped-spawn-half (bare word "done"
// inside "the scout pass is done and I'm moving to implementation") as
// already fixture 3 of sibling target 🎯T784, which fixes the reap decision
// on THAT specimen (a status naming a forward-looking next step, no
// attribution to another party). T784 is not achieved in this tree; this
// target does not re-fix it. What T829 does require of that shared control
// is the genuine terminal finish-report still reaping (🎯T165), asserted
// below.

func TestT829GenuineTerminalControlStillReaps(t *testing.T) {
	const report = "```jevons\njevons: kind finish-report\njevons: target T494.1.1\njevons: oracle-gate go-test exit=0 GREEN id=deadbeef tree=clean@abc123\njevons: silent-ledger none\n```\n\nImplementation landed and committed as abc1234567.\n\nGOAL_STATUS: complete"
	if !LooksLikeFinishedWorkReport(report) {
		t.Fatal("a genuine typed finish-report envelope must still reap")
	}
}

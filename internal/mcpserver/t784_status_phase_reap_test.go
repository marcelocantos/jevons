// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T784 mechanism specimen, 2026-09-21 23:26 (~/.jevons/logs/events.jsonl,
// read by the supervisor). jv-t762-dropped-spawn-half was reaped at
// 13:26:20Z on a turn whose opening sentence was "T494.1.1 is in progress:
// the scout pass is done and I'm moving to implementation." The bare word
// "done" inside "the scout pass is done" was the completion-claim hit — but
// the reap actually went through because "pass", one of hasOracleEvidence's
// bare-substring markers, matched the noun "scout pass" (a status phase, not
// a test verdict) and short-circuited hasFinishShape's bare-claim-clause
// check straight past the requirement that a non-bare clause carry oracle
// evidence or accepted-risk language. Fourth jevons-po seat lost mid-mission
// that night; nothing was committed for T494.1.1.
const t784ScoutPassStatusReport = "T494.1.1 is in progress: the scout pass is done and I'm moving to implementation."

func TestT784StatusPhaseWordDoesNotReadAsOracleEvidence(t *testing.T) {
	lower := asciiLower(t784ScoutPassStatusReport)
	if hasOracleEvidence(lower) {
		t.Fatal(`"the scout pass is done" read as oracle evidence — "pass" as a status-phase noun must not satisfy hasOracleEvidence`)
	}
}

func TestT784StatusPhaseReportDoesNotReap(t *testing.T) {
	if LooksLikeFinishedWorkReport(t784ScoutPassStatusReport) {
		marker, span, _, _ := FindCompletionClaim(t784ScoutPassStatusReport)
		t.Fatalf("a mid-mission status naming the next step (%q in %q) must not read as finished work", marker, span)
	}

	reg := t395Registry(t, "jv-t762-dropped-spawn-half")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t762-dropped-spawn-half", t784ScoutPassStatusReport, nil)
	if ok {
		t.Fatalf("ShouldAutoReapDoneWorkAgent reaped the status-phase report (reason %s)", reason)
	}

	s, reg2 := reapSinkServer(t, "jv-t762-dropped-spawn-half")
	s.agentEventSink("jv-t762-dropped-spawn-half")(claudia.Event{
		Type:       "assistant",
		Text:       t784ScoutPassStatusReport,
		StopReason: "end_turn",
	})
	if reg2.Def("jv-t762-dropped-spawn-half") == nil {
		t.Fatal("the sink deregistered a seat on a status update naming its next step")
	}
}

// A second, independent fixture from the same acceptance clause: a report
// citing a real GREEN gate id while naming the next piece of work ("T737 is
// running next") is a checkpoint, not a finish — it must not reap either.
const t784GateThenNextWorkReport = "🎯T737 GATE go-test-t737 exit=0 GREEN id=9f13c0a2. T737 is running next; the achieve for this one comes after that lands."

func TestT784GreenGateNamingNextWorkDoesNotReap(t *testing.T) {
	if LooksLikeFinishedWorkReport(t784GateThenNextWorkReport) {
		marker, span, _, _ := FindCompletionClaim(t784GateThenNextWorkReport)
		t.Fatalf("a GREEN-gate report that names the next piece of work (%q in %q) must not read as finished work", marker, span)
	}
}

// Control: a genuine terminal finish-report still reaps (🎯T165) — the T784
// fix narrows a false positive, it must not widen the veto.
func TestT784GenuineFinishStillReapsControl(t *testing.T) {
	const report = "My change for T494.1.1 is committed as abc1234567. Oracle: go test ./internal/mcpserver -run T494 GREEN. The mission is done."
	if !LooksLikeFinishedWorkReport(report) {
		t.Fatal("a genuine finish report with oracle evidence must still reap")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T750: a worker is reaped only on a completion claim it made.
//
// The specimen is jv-t745-busy-pane's 17:51:01Z checkpoint: four numbered
// "Still to do" items, a "Residual:" paragraph that satisfies hasAcceptedRisk,
// and a closing sentence whose only completion marker is negated. Verbatim
// except for the word "Checkpoint.", which is removed on purpose — with it,
// 🎯T716's ask class vetoes the reap and this test would pass without
// exercising the claim classifier at all. What is under test is that the
// CLAIM side declines, so the seat's survival does not depend on an ask
// phrase list happening to recognise the wording.
const t745BusyPaneReport = `Status: the ` + "`-clean`" + ` gate measured HEAD, which doesn't contain my changes yet, so I'm discarding it as evidence. The fix is committed as ` + "`477983f4`" + `, and I've confirmed it's reachable from HEAD.

**What changed**
- New ` + "`internal/mcpserver/t745_busy_pane.go`" + `. ` + "`paneBusyRendering`" + ` treats a stall as a busy pane only when the error classifies as ` + "`startup_stall`" + `.

**Which way it errs**
- It errs toward "busy". A wedged pane whose transcript moved within 2 minutes is held instead of flagged.
- Residual: an unusually fast repeat bounce that doesn't set the reminted flag could fake a fresh mtime.

**Still to do**
1. Run the wider gate: ` + "`bin/gate -clean -- go test ./internal/mcpserver`" + `.
2. Activate the development daemon (T632).
3. Observe the running surface (T552).
4. Send the finish-report envelope with the silent-decision ledger.

So this objective is not yet complete, and I'm not emitting a GOAL_STATUS line.`

// The control: the report that genuinely reaped this seat at 17:57:01Z. Its
// closing line is a bare, unquoted marker in the worker's own voice, and it
// must go on reaping — a fix that keeps everything keeps nothing.
const t745FinishedReport = `**Evidence:**
- ` + "`bin/gate -clean`" + ` id ` + "`94552994`" + `, GREEN at ` + "`clean@477983f4`" + `. It ran ` + "`TestT745*`" + ` in ` + "`internal/mcpserver`" + `.
- The wider gate over the whole package was killed by the harness timeout (T712), so I have not shown the whole package green.

**Residual (accepted risk):** I did not reproduce the specimen on the running surface. The behaviour is pinned by the table test, not observed live.

GOAL_STATUS: complete`

func TestT750QuotedMarkerDoesNotReap(t *testing.T) {
	if LooksLikeFinishedWorkReport(t745BusyPaneReport) {
		marker, span, _, _ := FindCompletionClaim(t745BusyPaneReport)
		t.Fatalf("the jv-t745-busy-pane checkpoint read as a finish; it would have been reaped on marker %q in %q", marker, span)
	}
	if _, _, _, ok := FindCompletionClaim(t745BusyPaneReport); ok {
		marker, span, _, _ := FindCompletionClaim(t745BusyPaneReport)
		t.Errorf("no asserted claim expected; log line would name marker %q in %q", marker, span)
	}
}

// The red witness. Without the mask, hasFinishShape read this text as a
// finish: an unmasked marker scan matches the negated "complete", and the
// "Residual:" paragraph satisfies hasAcceptedRisk, which short-circuits the
// shape test before the bare-claim-clause rule is ever consulted. Asserting
// both halves here keeps the defect documented in the suite rather than in a
// stash that no longer exists.
func TestT750WitnessTheUnmaskedScanMatched(t *testing.T) {
	lower := asciiLower(t745BusyPaneReport)
	if !hasAnyCompletionMarker(lower) {
		t.Fatal("specimen no longer carries a completion marker at all; the test has drifted off the shape it pins")
	}
	if !hasAcceptedRisk(lower) {
		t.Fatal("specimen no longer carries accepted-risk language; without it hasFinishShape never took the short-circuit that made this a finish")
	}
	if hasCompletionClaim(lower) {
		t.Fatal("masked scan still reads an asserted claim in a report whose only marker is negated")
	}
}

func TestT750AssertedMarkerStillReaps(t *testing.T) {
	if !LooksLikeFinishedWorkReport(t745FinishedReport) {
		t.Fatal("the 17:56:27 report ends on an unquoted GOAL_STATUS: complete in the worker's own voice; it must still reap")
	}
	marker, span, _, ok := FindCompletionClaim(t745FinishedReport)
	if !ok || marker != "complete" {
		t.Fatalf("expected the asserted marker to be named; got %q ok=%v", marker, ok)
	}
	if span != "GOAL_STATUS: complete" {
		t.Errorf("the log must name the line the worker emitted; got span %q", span)
	}
}

// Each shape on its own, so a regression names which region stopped masking.
func TestT750CitedMarkerShapes(t *testing.T) {
	cases := []struct {
		name   string
		report string
		finish bool
	}{
		{"quoted objective block",
			"Objective: Achieve 🎯T750. Work until evidenced complete or blocked. When complete emit exactly: GOAL_STATUS: complete\n\nOracle: go test ./internal/mcpserver -run T750, GREEN.\nI have started reading the classifier.", false},
		{"marker inside a code span",
			"I am adding a test that feeds `GOAL_STATUS: complete` to the reaper. Oracle: go test ./internal/mcpserver -run T750 is green so far. Nothing is landed.", false},
		{"marker inside a fenced block",
			"The specimen text was:\n\n```\nGOAL_STATUS: complete\n```\n\nOracle: go test ./internal/mcpserver -run T750, GREEN. I am still writing the fix.", false},
		{"marker inside a blockquote",
			"The seat reported:\n\n> All done, mission complete.\n\nOracle: go test ./internal/mcpserver -run T750, GREEN. I have not verified that claim.", false},
		{"marker inside a quoted span",
			`The log line read "GOAL_STATUS: complete" at offset 3910. Oracle: go test ./internal/mcpserver -run T750, GREEN. I am still investigating.`, false},
		{"negated in its own clause",
			"Residual: the wider gate is unrun. The objective is not complete and I am still working.", false},
		{"declining to emit the marker",
			"Residual: accepted risk on the live path. I am not emitting GOAL_STATUS: complete because the gate has not run.", false},

		{"bare done still reaps",
			"Done.", true},
		{"claim with oracle evidence still reaps",
			"The fix is committed as 477983f4. Oracle: go test ./internal/mcpserver -run T750, GREEN. Work is done.", true},
		{"claim with accepted risk still reaps",
			"Implementation complete. Residual: accepted risk — I did not reproduce the specimen on the running surface.", true},
		{"claim on a line that also quotes one still reaps",
			"The objective said: emit exactly GOAL_STATUS: complete.\n\nOracle: go test ./internal/mcpserver -run T750, GREEN. The work is done and committed as 477983f4.", true},
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

// claimScanText must not move any byte, or every offset a caller logs —
// FindCompletionClaim's, and the report_offset in the lifecycle record —
// silently indexes a different report than the one on disk.
func TestT750MaskPreservesOffsets(t *testing.T) {
	for _, s := range []string{t745BusyPaneReport, t745FinishedReport, "```\ncomplete\n```", "`done` and \"finished\""} {
		if got := len(claimScanText(s)); got != len(s) {
			t.Errorf("claimScanText changed length: got %d, want %d", got, len(s))
		}
	}
}

// The acceptance criterion names the reap, not the classifier: the seat is
// KEPT. Both arms run the whole path — ShouldAutoReapDoneWorkAgent for the
// decision and its reason, then the live event sink, which is what actually
// removes a name from the registry.
func TestT750SeatIsKeptOnAQuotedMarker(t *testing.T) {
	const agent = "jv-t745-busy-pane"
	reg := t395Registry(t, agent)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, agent, t745BusyPaneReport, nil)
	if ok {
		t.Fatalf("a report whose only completion marker is negated reaps (reason %s)", reason)
	}
	if reason != "not_finished_work_report" {
		t.Errorf("reason = %q, want not_finished_work_report", reason)
	}

	s, reg2 := t471SinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{
		Type:       "assistant",
		Text:       t745BusyPaneReport,
		StopReason: "end_turn",
	})
	if reg2.Def(agent) == nil {
		t.Fatal("the sink deregistered a seat that had just said it was not finished")
	}
}

func TestT750SeatIsReapedOnItsOwnMarker(t *testing.T) {
	const agent = "jv-t745-busy-pane"
	reg := t395Registry(t, agent)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, agent, t745FinishedReport, nil)
	if !ok {
		t.Fatalf("the report that ends on an unquoted GOAL_STATUS: complete did not reap (reason %s)", reason)
	}

	s, reg2 := t471SinkServer(t, agent)
	s.agentEventSink(agent)(claudia.Event{
		Type:       "assistant",
		Text:       t745FinishedReport,
		StopReason: "end_turn",
	})
	if reg2.Def(agent) != nil {
		t.Fatal("the sink kept a seat that claimed completion in its own voice")
	}
}

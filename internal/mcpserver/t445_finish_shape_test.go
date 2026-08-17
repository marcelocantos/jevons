// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T445: an unrecognised worker turn is not read as a finish.
//
// The 🎯T439/🎯T395 ask classes are phrase lists, so an ask worded outside
// them used to fall through to "completion word anywhere ⇒ finish" — the
// dangerous default, because reading an ask as a finish REAPS the agent
// mid-question. These turns are asks in wording no current list matches,
// each carrying incidental completion vocabulary; every one must classify
// NOT-a-finish. Red against the pre-fix tree.
var t445UnlistedAskTurns = []string{
	// A named missing input, worded outside requestMarkers.
	"Config migration done. One thing I need from you: the prod credentials for the second half.",
	// A question with no question mark.
	"The refactor is complete, but which of the two schemas is canonical for the fleet registry — I can't tell from here.",
	// A confirmation ask outside the confirm phrases.
	"Tests are done locally. Could you check the daily daemon is safe to bounce right now",
	// An ask embedded mid-report, more than closingQuestionTailLines from the end.
	"Phase 1 finished.\nDo you want the schema change in this PR or a follow-up?\nEvidence noted so far:\n- parser boundaries mapped\n- fixture turns collected\n- nothing checked on the daemon path yet",
	// Waiting on the parent, worded outside the waiting phrases.
	"Waiting on a call from you before the merge — done otherwise.",
	// A handed-over decision, worded outside decisionRequestMarkers.
	"All tests done; the remaining step needs an owner decision on the port allocation, over to you.",
	// An input request in imperative form outside the send/provide phrases.
	"Hand me the updated brief when you have it — I've gone as far as I can; recon complete.",
}

func TestT445UnlistedAskTurnsAreNotFinishes(t *testing.T) {
	for _, turn := range t445UnlistedAskTurns {
		if LooksLikeFinishedWorkReport(turn) {
			t.Errorf("unlisted ask read as finish:\n%q", turn)
		}
	}
}

// 🎯T445: the real unrecognised turns from the jv-t497-checkpoint-reap
// incident stream — neither asks nor finishes, just turns. They carried no
// completion claim, so they were safe even pre-fix; pinned so they stay safe.
func TestT445RealUnrecognisedTurnsAreNotFinishes(t *testing.T) {
	for _, turn := range []string{
		"No response requested.", // delivered four times, 22 bytes each
		"The probe returned 200, but it may have hit the old daemon that was still serving. Checking the restart log to confirm before I read anything into it…",
	} {
		if LooksLikeFinishedWorkReport(turn) {
			t.Errorf("unrecognised turn read as finish:\n%q", turn)
		}
	}
}

// 🎯T445: the report that got this target's own first seat falsely reaped as
// bare_done_completion — an explicit depth-ceiling checkpoint (stored report
// 20260817T112032Z-0cdfeb94). It must classify NOT-a-finish on every layer
// independently: the ask classifier (🎯T497 checkpoint declaration), and —
// defence in depth — the positive finish shape, so that even a build in which
// every ask phrase list misses cannot read it as done.
func TestT445ReapedCheckpointReportIsNotAFinish(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "t445_reaped_checkpoint_report.md"))
	if err != nil {
		t.Fatal(err)
	}
	report := string(b)
	if LooksLikeFinishedWorkReport(report) {
		t.Fatal("reaped checkpoint fixture reads as finish")
	}
	if ask := ClassifyReportAsk(report); ask == AskNone {
		t.Fatal("checkpoint fixture not recognised as an ask/checkpoint")
	}
	reg := regWithTree(t)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
	if ok {
		t.Fatalf("checkpoint fixture reaps (reason %s)", reason)
	}
}

// 🎯T445 preserved behaviour: a positive finish shape still reaps. The bare
// claim clause is the 🎯T195 imperfect report; claim plus oracle evidence is
// the 🎯T165 proper one; claim plus accepted-risk is the 🎯T31.1 residual
// path. The fail-safe narrows what counts as a finish — it does not disable
// reaping.
func TestT445PositiveFinishShapesStillReap(t *testing.T) {
	for _, report := range []string{
		"Done.",
		"Done. Mission complete. Target achieved.",
		"Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS",
		"complete with accepted risk: owner smoke residual (class-3)",
	} {
		if !LooksLikeFinishedWorkReport(report) {
			t.Errorf("positive finish shape not read as finish:\n%q", report)
		}
	}
	reg := regWithTree(t)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", "Done. All finished.", func(string) bool { return false })
	if !ok {
		t.Fatalf("bare done must still reap (T195); reason %s", reason)
	}
	if !strings.Contains(reason, "bare_done") {
		t.Fatalf("bare done reason = %s", reason)
	}
}

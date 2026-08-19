// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T497: a depth-ceiling checkpoint report is not a finish.
//
// The 🎯T392.4 ceiling ask tells a worker to "Reach a checkpoint and END YOUR
// TURN" — write down where you are, state the next step, end the turn, be
// resumed. jv-t496-owner-reply did exactly that: its report opens "Checkpoint —
// ending this turn at the depth ceiling", lists next steps for the successor
// turn, and closes "No files modified yet; nothing to commit." The reap path
// read the progress header "Done so far:" as a completion claim, found
// "commit" nearby, and deregistered the seat as finished_work with zero
// commits — the worker was destroyed for complying with the ceiling.
//
// The fixture is the worker's actual stored report
// (~/.jevons/agent-reports/jv-t496-owner-reply/20260817T100508Z-c7a72ac9.json),
// not a paraphrase.

func loadCheckpointReport(t *testing.T) string {
	return loadT497Fixture(t, "t497_jv_t496_checkpoint_report.md", 1000)
}

func loadT497Fixture(t *testing.T, name string, minBytes int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if len(b) < minBytes {
		t.Fatalf("fixture %s is too short to be the real one: %d bytes", name, len(b))
	}
	return string(b)
}

// TestT497CheckpointReportIsTheIncidentShape pins the properties that made the
// real report reap. If the fixture ever loses them it stops testing anything.
func TestT497CheckpointReportIsTheIncidentShape(t *testing.T) {
	report := loadCheckpointReport(t)
	lower := strings.ToLower(report)
	if !hasCompletionClaim(lower) {
		t.Fatal("fixture no longer carries a completion word — it cannot reproduce the incident")
	}
	if !strings.HasPrefix(strings.TrimSpace(lower), "checkpoint") {
		t.Fatal("fixture no longer opens with its checkpoint declaration")
	}
	if !strings.Contains(lower, "nothing to commit") {
		t.Fatal("fixture no longer states it has nothing to commit")
	}
	if LooksLikeFinishedWorkReport(report) {
		t.Fatal("🎯T497: a depth-ceiling checkpoint report is not a finished-work report")
	}
}

// TestT497CheckpointDoesNotReap is the acceptance oracle run through the whole
// reap decision, both ways: the real checkpoint must not reap, and a real
// finish — commit SHA plus a GATE GREEN line (🎯T386/🎯T396) — still must.
func TestT497CheckpointDoesNotReap(t *testing.T) {
	cases := []struct {
		name     string
		report   string
		wantReap bool
		wantAsk  ReportAskClass
	}{
		{
			name:     "jv-t496 depth-ceiling checkpoint does not reap",
			report:   loadCheckpointReport(t),
			wantReap: false,
			wantAsk:  AskCheckpoint,
		},
		{
			// jv-t498-test-web 20260817T101623Z-7e905ae6: resumed mid-mission,
			// future-tense next steps, no SHA of its own, no gate run.
			name:     "jv-t498 mid-mission resume does not reap",
			report:   loadT497Fixture(t, "t497_jv_t498_midmission_report.md", 200),
			wantReap: false,
			wantAsk:  AskExplicitIncomplete,
		},
		{
			// jv-t498-test-web 20260817T101708Z-32040136: the hardest not-done
			// shape — it carries a REAL commit SHA, but opens "Checkpoint —
			// commit is landed, only the gate run remains" and closes "Still in
			// progress — acceptance gate not yet run". A hash alone is not a
			// finish.
			name:     "jv-t498 checkpoint with a real SHA does not reap",
			report:   loadT497Fixture(t, "t497_jv_t498_sha_checkpoint_report.md", 500),
			wantReap: false,
			wantAsk:  AskCheckpoint,
		},
		{
			// Cite oracle evidence without an invented GATE id: a fake id trips
			// 🎯T386 attestation_unknown, and 🎯T470 refuses to reap any
			// false-green-flagged report (even one that otherwise looks done).
			name: "genuine finish with SHA and go test PASS reaps",
			report: "🎯T496 done. Landed as commit 3f2c1a9 (ancestor of HEAD verified).\n" +
				"go test ./internal/mcpserver -run T497 PASS\n" +
				"Hermetic oracle covers the paint path both ways.",
			wantReap: true,
			wantAsk:  AskNone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := t395Registry(t, "jv-t496-owner-reply")
			got, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t496-owner-reply", tc.report, nil)
			if got != tc.wantReap {
				t.Fatalf("ShouldAutoReapDoneWorkAgent = %v (%s), want %v", got, reason, tc.wantReap)
			}
			if ask := ClassifyReportAsk(tc.report); ask != tc.wantAsk {
				t.Fatalf("ClassifyReportAsk = %s, want %s", ask, tc.wantAsk)
			}
		})
	}
}

// TestT497CheckpointShapes covers the checkpoint grammar independently of the
// reap path: a declaration is a line that IS the word, not prose that contains
// it (the 🎯T446 lesson applied to this fix's own vocabulary — a finish report
// ABOUT checkpoint handling must still reap).
func TestT497CheckpointShapes(t *testing.T) {
	cases := []struct {
		name   string
		report string
		want   ReportAskClass
	}{
		{"opening declaration with em dash", "Checkpoint — parser mapped; writer next.\nDone so far: the map.", AskCheckpoint},
		{"opening declaration with colon", "Checkpoint: wired the decoder. Next step: the encoder.", AskCheckpoint},
		{"bare banner line", "Recon complete on the wire path.\n\nCHECKPOINT", AskCheckpoint},
		{"emphasised banner line", "**Checkpoint.** Ran out of depth; successor picks up at step 3.", AskCheckpoint},
		{"ceiling echo without the word checkpoint", "Ending this turn at the depth ceiling. Step 3 next.", AskCheckpoint},
		{"status in progress", "Status: in progress. Decoder wired; encoder not started.", AskExplicitIncomplete},
		{"nothing to commit", "Survey ran the full path. No files modified yet; nothing to commit.", AskExplicitIncomplete},
		{"mention is not a declaration", "Done. Commit 4f2b8c1; go test PASS. Checkpoint reports now classify as open work.", AskNone},
		{"finish about the ceiling still finishes", "Done. Commit 4f2b8c1; go test PASS. The ceiling ask no longer reaps compliant workers.", AskNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyReportAsk(tc.report); got != tc.want {
				t.Fatalf("ClassifyReportAsk = %s, want %s", got, tc.want)
			}
			wantFinish := tc.want == AskNone && hasCompletionClaim(strings.ToLower(tc.report))
			if got := LooksLikeFinishedWorkReport(tc.report); got != wantFinish {
				t.Fatalf("LooksLikeFinishedWorkReport = %v, want %v", got, wantFinish)
			}
		})
	}
}

// TestT497TrueFinishIsNotAnAsk pins jv-t496's actual finish report
// (20260817T101257Z-e9d11937: completion prose, real SHA, GATE … exit=0 GREEN)
// as AskNone: none of the checkpoint or incomplete markers may creep wide
// enough to read a genuine finish as open work.
//
// It is deliberately NOT run through the reap decision: the report says
// "landed" but never uses a completion-claim word ("done", "complete", …), and
// 🎯T195 reaps only on a claim — a pre-existing gap this target neither widens
// nor pins as correct.
func TestT497TrueFinishIsNotAnAsk(t *testing.T) {
	report := loadT497Fixture(t, "t497_jv_t496_true_finish_report.md", 1000)
	if got := ClassifyReportAsk(report); got != AskNone {
		t.Fatalf("ClassifyReportAsk = %s, want none — a genuine finish must not read as an ask", got)
	}
}

// TestT497ReasonNamesTheCheckpoint pins the lifecycle-log reason so a skipped
// reap is attributable from the record alone (🎯T439's span discipline).
func TestT497ReasonNamesTheCheckpoint(t *testing.T) {
	reg := t395Registry(t, "jv-t496-owner-reply")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t496-owner-reply", loadCheckpointReport(t), nil)
	if ok {
		t.Fatal("checkpoint report reaped")
	}
	if reason != "awaits_overseer_checkpoint" {
		t.Fatalf("reason = %q, want awaits_overseer_checkpoint", reason)
	}
	f := ClassifyReportAskDetail(loadCheckpointReport(t))
	if f.Marker != "checkpoint" || !strings.Contains(strings.ToLower(f.Span), "checkpoint") {
		t.Fatalf("finding does not point at the declaration: marker=%q span=%q", f.Marker, f.Span)
	}
}

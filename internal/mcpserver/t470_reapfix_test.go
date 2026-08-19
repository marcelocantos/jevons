// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T470: mid-mission checkpoints that deny completion must not reap, a
// false-green-flagged report must not reap, and the decision names the
// sentence it matched.

func loadT470Fixture(t *testing.T, name string, minBytes int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if len(b) < minBytes {
		t.Fatalf("fixture %s too short (%d bytes) to be the incident report", name, len(b))
	}
	return string(b)
}

func TestT470IncidentReportsAreNotFinishedWork(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		min     int
		wantAsk ReportAskClass
		deny    string // explicit non-completion phrase that must remain
	}{
		{
			name:    "jv-t390-plan-usage 20260815T062910Z-09c58493",
			file:    "t470_jv_t390_checkpoint_report.md",
			min:     2000,
			wantAsk: AskCheckpoint, // opens with Checkpoint — …; also denies achievement
			deny:    "nothing here is achieved",
		},
		{
			name:    "jv-t391-guard-all-paths 20260815T062936Z-8e58ef45",
			file:    "t470_jv_t391_checkpoint_report.md",
			min:     2000,
			wantAsk: AskExplicitIncomplete,
			deny:    "oracles are missing",
		},
	}
	reg := regWithTree(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := loadT470Fixture(t, tc.file, tc.min)
			if !strings.Contains(strings.ToLower(report), strings.ToLower(tc.deny)) {
				t.Fatalf("fixture lost its non-completion sentence %q", tc.deny)
			}
			if !hasCompletionClaim(asciiLower(report)) {
				t.Fatal("fixture no longer carries a completion word — cannot reproduce the incident")
			}
			if LooksLikeFinishedWorkReport(report) {
				t.Fatal("incident report classified as finished_work")
			}
			ask := ClassifyReportAsk(report)
			if ask != tc.wantAsk {
				t.Fatalf("ask class = %s, want %s", ask, tc.wantAsk)
			}
			ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
			if ok {
				t.Fatalf("incident report reaps (reason %s)", reason)
			}
		})
	}
}

// Control: a genuine finish report still reaps (clause 1).
func TestT470GenuineFinishStillReaps(t *testing.T) {
	reg := regWithTree(t)
	// No GATE id line: citing an invented id trips attestation_unknown (🎯T386).
	report := "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T470 PASS"
	if !LooksLikeFinishedWorkReport(report) {
		t.Fatal("genuine finish not read as finished_work")
	}
	if flags := FalseGreenFlags(report); len(flags) > 0 {
		t.Fatalf("control finish unexpectedly false-green flagged: %v", flags)
	}
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
	if !ok {
		t.Fatalf("genuine finish did not reap (reason %s)", reason)
	}
}

// Clause 2: any 🎯T386 false-green flag blocks auto-reap even when the prose
// otherwise looks like a finish (the jv-t391 shape: claim + GATE RED).
func TestT470FalseGreenFlaggedReportNeverReaps(t *testing.T) {
	reg := regWithTree(t)
	// Finish-shaped (bare claim + oracle markers) but cites a RED gate — the
	// same contradiction that flagged jv-t391 attestation_not_green before it
	// was reaped finished_work 447ms later.
	report := "Done. Mission complete. SHA abcdef0123456. GATE go-test exit=1 RED id=2817ca36."
	if !LooksLikeFinishedWorkReport(report) {
		t.Fatal("control must look like finished_work before the false-green veto")
	}
	flags := FalseGreenFlags(report)
	if len(flags) == 0 {
		t.Fatal("control must carry a false-green flag")
	}
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, func(string) bool { return false })
	if ok {
		t.Fatal("false-green-flagged report reaped as finished_work")
	}
	if !strings.HasPrefix(reason, "false_green_") {
		t.Fatalf("reason = %q, want false_green_*", reason)
	}
}

// Clause 3: the reap decision names the sentence it matched, not a 200-char
// window around an offset.
func TestT470ReapDecisionNamesMatchedSentence(t *testing.T) {
	report := loadT470Fixture(t, "t470_jv_t391_checkpoint_report.md", 2000)
	marker, span, offset, ok := FindCompletionClaim(report)
	if !ok {
		t.Fatal("expected a completion claim in the incident fixture")
	}
	if marker != "complete" {
		t.Fatalf("marker = %q, want complete (the incident word)", marker)
	}
	if offset < 0 || offset+len(span) > len(report) || report[offset:offset+len(span)] != span {
		t.Fatalf("span not at offset %d", offset)
	}
	// Must be the clause carrying "complete", not a 200-byte mid-cut window.
	if !strings.Contains(strings.ToLower(span), "complete") {
		t.Fatalf("span lost the claim word: %q", span)
	}
	if strings.HasPrefix(span, "ed.") || strings.HasSuffix(span, "Let me") {
		t.Fatalf("span looks like the old 200-byte window clip: %q", span)
	}
	if len(span) > 400 {
		t.Fatalf("span is %d bytes — expected a sentence/clause, not a paragraph", len(span))
	}
	// Semicolon breaks the clause so the claim sentence is isolable.
	lower := strings.ToLower(span)
	if !strings.Contains(lower, "implementation is complete") {
		t.Fatalf("span = %q, want the 'Implementation is complete' clause", span)
	}

	fields := reapDecisionFields("worker", "finished_work", "Done. Ship it tomorrow.")
	spanField, _ := fields["report_span"].(string)
	if spanField != "Done." && !strings.HasPrefix(spanField, "Done") {
		// Bare "Done." is the whole sentence.
		if fields["claim_marker"] != "done" {
			t.Fatalf("fields=%v", fields)
		}
	}
}

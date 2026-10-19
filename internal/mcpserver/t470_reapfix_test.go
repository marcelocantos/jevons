// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
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
			wantAsk: AskNone, // a checkpoint sentence is not an ask
			deny:    "nothing here is achieved",
		},
		{
			name:    "jv-t391-guard-all-paths 20260815T062936Z-8e58ef45",
			file:    "t470_jv_t391_checkpoint_report.md",
			min:     2000,
			wantAsk: AskNone, // a jammed checkpoint sentence is not an ask
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
			if !hasAnyCompletionMarker(asciiLower(report)) {
				t.Fatal("fixture no longer carries a completion word — cannot reproduce the incident")
			}
			if hasCompletionClaim(asciiLower(report)) {
				t.Fatal("a completion sentence is a completion claim")
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
	report := declaredFinishReport("T470", "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T470 PASS")
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

// Clause 2: a declared finish whose declared evidence is contradicted never
// auto-reaps (the jv-t391 shape: claim + RED gate).
//
// 🎯T766.4.1: the RED gate is the envelope's gate-id slot, resolved against
// the store. Before, it was a GATE line in the payload read by the banner's
// regexes; that expectation was wrong once a control may not decide by
// English, and the typed fact is what carries the jv-t391 contradiction now.
func TestT470FalseGreenFlaggedReportNeverReaps(t *testing.T) {
	t76641Store(t, t76641Record("2817ca36", gate.VerdictRed, 1))
	reg := regWithTree(t)
	// A declared finish citing a RED gate — the same contradiction that
	// flagged jv-t391 attestation_not_green before it was reaped
	// finished_work 447ms later. The veto runs only after a declared finish
	// (🎯T766.4).
	report := declaredGateFinishReport("T470", "2817ca36", envelope.VerdictGreen, "Done. Mission complete.")
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

// Clause 3: a sentence is not a completion claim. The fixture still
// carries the incident clause. The reap log does not copy a prose span
// (🎯T766.4).
func TestT470ReapDecisionNamesMatchedSentence(t *testing.T) {
	report := loadT470Fixture(t, "t470_jv_t391_checkpoint_report.md", 2000)
	if _, _, _, ok := FindCompletionClaim(report); ok {
		t.Fatal("a sentence is a completion claim")
	}
	if !strings.Contains(strings.ToLower(report), "implementation is complete") {
		t.Fatal("fixture lost the incident clause")
	}

	fields := reapDecisionFields("worker", "finished_work", "Done. Ship it tomorrow.")
	if _, ok := fields["claim_marker"]; ok {
		t.Fatalf("reap fields still name a prose claim: %v", fields)
	}
	if _, ok := fields["report_span"]; ok {
		t.Fatalf("reap fields still carry a prose span: %v", fields)
	}
	if fields["done_class"] != "unknown" {
		t.Fatalf("unenveloped prose done_class=%v", fields["done_class"])
	}
}

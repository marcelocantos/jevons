// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
)

// 🎯T395 fixtures. The four the acceptance names, in one table, each run
// through the whole reap decision — not just the string classifier — so a
// regression anywhere on the path fails here.
//
// The diagnosis fixture is jv-t387-spawn-turn-begins' actual stranded report,
// read out of its chatlog, not a synthetic paraphrase. It is the shape that
// caused the incident: opens "Diagnosis complete", 4.7 KB of first-hand
// evidence, closes by handing a boundary decision to the overseer.
//
// wantReap is the load-bearing column and it runs both ways: two fixtures must
// reap and two must not. A classifier that stops reaping everything fails the
// genuine-finish and bare-done rows exactly as loudly as the pre-fix classifier
// fails the diagnosis and blocked rows.
func t395Fixtures(t *testing.T) []struct {
	name     string
	report   string
	wantReap bool
	wantAsk  ReportAskClass
} {
	t.Helper()
	return []struct {
		name     string
		report   string
		wantReap bool
		wantAsk  ReportAskClass
	}{
		{
			name: "genuine finish with SHA and green oracle reaps",
			report: "🎯T395 done. Commit 4f2b8c1 lands the classifier and its oracle.\n" +
				"`go test ./internal/mcpserver/ -run T395` PASS (8 cases, RED against the\n" +
				"pre-fix tree and against the never-reap mutation).",
			wantReap: true,
			wantAsk:  AskNone,
		},
		{
			name:     "diagnosis ending in a decision request does not reap",
			report:   loadStrandedReport(t),
			wantReap: false,
			wantAsk:  AskExplicitIncomplete,
		},
		{
			name: "blocked pending an answer does not reap",
			report: "Mission complete up to the fork and then blocked pending your answer:\n" +
				"the fix is either a jevons repin or a claudia change, and I am not\n" +
				"proceeding until you pick. Nothing committed.",
			wantReap: false,
			wantAsk:  AskExplicitIncomplete,
		},
		{
			name:     "imperfect bare done still reaps (🎯T165 / 🎯T195)",
			report:   "Done.",
			wantReap: true,
			wantAsk:  AskNone,
		},
	}
}

// t395Registry is one plain work leaf — no descendants, no durable role — so
// the only thing that can change the reap decision is the report itself.
func t395Registry(t *testing.T, name string) *claudia.Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-w",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func loadStrandedReport(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "t395_jv_t387_stranded_report.md"))
	if err != nil {
		t.Fatalf("read stranded report fixture: %v", err)
	}
	if len(b) < 1000 {
		t.Fatalf("stranded report fixture is too short to be the real one: %d bytes", len(b))
	}
	return string(b)
}

// TestT395ReapFixtures is the acceptance oracle: the full reap decision over a
// registry with one plain work agent.
func TestT395ReapFixtures(t *testing.T) {
	for _, tc := range t395Fixtures(t) {
		t.Run(tc.name, func(t *testing.T) {
			reg := t395Registry(t, "jv-t387-spawn-turn-begins")

			got, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t387-spawn-turn-begins", tc.report, nil)
			if got != tc.wantReap {
				t.Fatalf("ShouldAutoReapDoneWorkAgent = %v (%s), want %v", got, reason, tc.wantReap)
			}
			if ask := ClassifyReportAsk(tc.report); ask != tc.wantAsk {
				t.Fatalf("ClassifyReportAsk = %s, want %s", ask, tc.wantAsk)
			}
			if !tc.wantReap && ClassifyReportAsk(tc.report) != AskNone {
				if !strings.HasPrefix(reason, "awaits_overseer_") {
					t.Fatalf("non-reap reason = %q, want awaits_overseer_*", reason)
				}
			}
		})
	}
}

// TestT395StrandedReportIsTheIncidentShape pins the two properties that made
// the real report reap: it contains a completion word, and the old classifier
// looked no further. If the fixture ever loses its completion word it stops
// being a regression test for anything.
func TestT395StrandedReportIsTheIncidentShape(t *testing.T) {
	report := loadStrandedReport(t)
	if !hasCompletionClaim(strings.ToLower(report)) {
		t.Fatal("fixture no longer carries a completion word — it cannot reproduce the incident")
	}
	if !strings.Contains(strings.ToLower(report), "reporting before changing anything") {
		t.Fatal("fixture no longer carries the report-before-acting statement")
	}
	if LooksLikeFinishedWorkReport(report) {
		t.Fatal("🎯T395: a report that asks for a boundary decision is not a finished-work report")
	}
}

// TestT395AsksAreNotFinishes covers the ask classes independently of the reap
// path, including the closing-question form the acceptance names first.
func TestT395AsksAreNotFinishes(t *testing.T) {
	cases := []struct {
		name   string
		report string
		want   ReportAskClass
	}{
		{"closing question", "Done with the scan. Which of the two boundaries do you want?", AskClosingQuestion},
		{"closing question with emphasis", "Complete.\n\n**Repin claudia first, or land cl-t21 first?**", AskClosingQuestion},
		{"mid-report question is not a closing act", "Why did T305 miss it? Because Send()==nil is weak.\nDone. Commit abc1234, go test PASS.", AskNone},
		{"go-ahead request", "Analysis complete. Say go and I'll land it.", AskDecisionRequest},
		{"holding", "Work is done up to the fork; holding here per your instruction.", AskDecisionRequest},
		{"reported before acting", "Diagnosis complete. Reporting before changing anything, as instructed.", AskExplicitIncomplete},
		{"plain finish asks nothing", "Done. Commit 4f2b8c1; go test ./internal/mcpserver/ PASS.", AskNone},
		{"polite follow-up offer is still a finish", "Done. Commit 4f2b8c1, go test PASS. Let me know if you want the follow-up target filed.", AskNone},
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

// TestT395NegatedClaimsAreNotClaims covers the words that assert the opposite
// of completion and were read as completion because the claim match was a
// substring: "incomplete" contains "complete", "unfinished" contains
// "finished", "abandoned" contains "done".
func TestT395NegatedClaimsAreNotClaims(t *testing.T) {
	for _, report := range []string{
		"The migration is incomplete.",
		"Left unfinished; ran out of budget.",
		"I abandoned the worktree approach and started over.",
		"Coverage is incomplete for the truncation path.",
	} {
		if hasCompletionClaim(strings.ToLower(report)) {
			t.Errorf("%q reads as a completion claim", report)
		}
		if LooksLikeFinishedWorkReport(report) {
			t.Errorf("%q reads as a finished-work report", report)
		}
	}
	// The words still match when they really are the claim.
	for _, report := range []string{"Done.", "Complete.", "All finished."} {
		if !hasCompletionClaim(strings.ToLower(report)) {
			t.Errorf("%q must still read as a completion claim", report)
		}
	}
}

// TestT395AskVetoIsOneWay is acceptance clause 3 written as an executable
// property rather than a table: the veto may only ever REMOVE a reap, never
// add one. Appending an ask to any report — one that would reap and one that
// would not — must leave it not reaping.
//
// This is the difference between a classifier that is merely more accurate and
// one whose failure direction is safe. A future marker added to either list can
// make the classifier wrong here, but it cannot make it wrong in the direction
// that destroys an agent mid-conversation.
func TestT395AskVetoIsOneWay(t *testing.T) {
	reports := []string{
		"Done.",
		"Complete — commit 9ab3f21, make test green.",
		"Achieved with accepted risk: owner smoke residual (class-3).",
		"All finished, ready for review.",
		"still reading the tree",
		"",
	}
	asks := []string{
		"Which boundary do you want?",
		"Say go and I'll land it.",
		"Holding here per your instruction.",
		"Reporting before changing anything, as instructed.",
		"Blocked pending your answer.",
		"Should I repin claudia first?",
	}
	for _, r := range reports {
		for _, a := range asks {
			if ClassifyReportAsk(a) == AskNone {
				t.Fatalf("ask fixture %q is not classified as an ask — the property is vacuous", a)
			}
			withAsk := strings.TrimSpace(r + "\n\n" + a)
			reg := t395Registry(t, "jv-worker")
			ok, _ := ShouldAutoReapDoneWorkAgent(reg, "jv-worker", withAsk, nil)
			if ok {
				t.Errorf("🎯T395 clause 3 violated: %q + ask %q reaps", r, a)
			}
		}
	}
}

// TestT395TruncatedReportIsNotAFinish composes with 🎯T388: a report that
// arrives visibly cut may have had its ask in the missing middle, so its intent
// is unknown — and unknown resolves toward keeping the agent.
func TestT395TruncatedReportIsNotAFinish(t *testing.T) {
	full := "Done. Commit 4f2b8c1.\n" + strings.Repeat("evidence line\n", 400) +
		"Which boundary do you want?"
	cut := agentreport.Elide(full, agentreport.DeliveryBound, agentreport.Handle{}).Text
	if !agentreport.IsTruncatedDelivery(cut) {
		t.Fatal("fixture did not truncate — 🎯T388 bound or marker changed")
	}
	if got := ClassifyReportAsk(cut); got != AskTruncated {
		t.Fatalf("ClassifyReportAsk(cut) = %s, want %s", got, AskTruncated)
	}
	if LooksLikeFinishedWorkReport(cut) {
		t.Fatal("🎯T395/🎯T388: a visibly cut report is not a finished-work report")
	}
}

// TestT395ReapStillFiresForOrdinaryFinishes is the over-broadness guard in the
// reap path itself: durable roles and descendants aside, a plain work agent
// that says it is done and asks nothing must still leave the fleet. Without
// this, "never reap" would pass every other test in this file.
func TestT395ReapStillFiresForOrdinaryFinishes(t *testing.T) {
	finishes := []string{
		"Done.",
		"All finished, ready for review.",
		"Complete — commit 9ab3f21, make test green.",
		"Achieved with accepted risk: owner smoke residual (class-3).",
	}
	for _, report := range finishes {
		reg := t395Registry(t, "jv-worker")
		ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-worker", report, nil)
		if !ok {
			t.Fatalf("report %q did not reap (%s) — 🎯T165 / 🎯T195 must survive the T395 narrowing", report, reason)
		}
	}
}

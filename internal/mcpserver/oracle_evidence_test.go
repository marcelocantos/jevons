// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "testing"

// 🎯T31.1: pure classifier for finish reports (oracle vs accepted-risk vs bare done).
func TestClassifyCompletionReport(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want CompletionEvidenceClass
	}{
		{"empty", "", CompletionNoClaim},
		{"status only", "still reading the tree", CompletionNoClaim},
		{"bare done", "Done. Mission complete.", CompletionBareDone},
		{"bare finished", "All finished, ready for review.", CompletionBareDone},
		{"oracle go test", "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T31 PASS", CompletionOracleEvidence},
		{"oracle make test", "complete — make test-web green", CompletionOracleEvidence},
		{"oracle word", "Oracle: node web/scripts/foo_test.js PASS", CompletionOracleEvidence},
		{"accepted risk", "Done with accepted risk: owner smoke residual (class-3)", CompletionAcceptedRisk},
		{"class-3 only", "Residual: isolated class-3 human gate for visual feel", CompletionAcceptedRisk},
		{"accepted-risk hyphen", "accepted-risk: no hermetic path for this spike", CompletionAcceptedRisk},
		{"risk beats bare claim", "finished; accepted risk logged", CompletionAcceptedRisk},
		{"oracle beats bare claim", "done; hermetic TestEnsureFleetBriefInjectsOnce green", CompletionOracleEvidence},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyCompletionReport(tc.in)
			if got != tc.want {
				t.Fatalf("ClassifyCompletionReport(%q)=%s want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestLooksLikeBareDoneAndHasOracleOrRisk(t *testing.T) {
	if !LooksLikeBareDone("Done, nothing else.") {
		t.Fatal("expected bare done")
	}
	if LooksLikeBareDone("Done. go test ./... PASS") {
		t.Fatal("oracle evidence must not look bare")
	}
	if !HasOracleOrRisk("SHA deadbeef make test green") {
		t.Fatal("expected oracle or risk")
	}
	if !HasOracleOrRisk("accepted risk residual for owner") {
		t.Fatal("expected risk path")
	}
	if HasOracleOrRisk("done") {
		t.Fatal("bare done is not oracle-or-risk")
	}
}

func TestCompletionEvidenceClassString(t *testing.T) {
	if CompletionBareDone.String() != "bare_done" {
		t.Fatalf("got %q", CompletionBareDone.String())
	}
	if CompletionOracleEvidence.String() != "oracle_evidence" {
		t.Fatalf("got %q", CompletionOracleEvidence.String())
	}
}

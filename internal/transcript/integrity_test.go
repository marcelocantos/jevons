// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import "testing"

func userTurn(text string) Entry {
	return Entry{Type: "user", Role: "user", Text: text, IsUserTurn: true}
}
func asst(text string) Entry { return Entry{Type: "assistant", Role: "assistant", Text: text} }

func kinds(issues []IntegrityIssue) map[IssueKind]int {
	m := map[IssueKind]int{}
	for _, i := range issues {
		m[i.Kind]++
	}
	return m
}

// The exact corruption the owner hit: a recalled message re-sent came
// back as its own text doubled, and as a duplicate turn.
func TestCheckUserTurns_RealBug(t *testing.T) {
	const msg = "Continuing testing... Explain this repo to me."

	entries := []Entry{
		userTurn(msg),
		asst("Since you said \"this repo\"…"),
		userTurn(msg + msg), // the doubled turn
	}
	got := kinds(CheckUserTurns(entries))
	if got[IssueSelfConcatenation] != 1 {
		t.Fatalf("expected 1 self-concatenation, got %d (issues: %+v)", got[IssueSelfConcatenation], CheckUserTurns(entries))
	}
}

func TestCheckUserTurns_DuplicateTurn(t *testing.T) {
	entries := []Entry{
		userTurn("spawn a worker for tern"),
		userTurn("spawn a worker for tern"), // identical consecutive
	}
	if n := kinds(CheckUserTurns(entries))[IssueDuplicateTurn]; n != 1 {
		t.Fatalf("expected 1 duplicate-turn, got %d", n)
	}
}

func TestCheckUserTurns_Clean(t *testing.T) {
	entries := []Entry{
		userTurn("explain this repo to me"),
		asst("jevons is your butler…"),
		userTurn("now spawn a worker for tern"),
		asst("done"),
		// Naturally repetitive but not corrupt — must NOT be flagged.
		userTurn("the the quick brown fox jumps over the lazy dog several times"),
		userTurn("go go go"),
	}
	if issues := CheckUserTurns(entries); len(issues) != 0 {
		t.Fatalf("clean transcript flagged: %+v", issues)
	}
}

// A message that is exactly its own half repeated (no trailing text).
func TestCheckUserTurns_ExactDouble(t *testing.T) {
	half := "please run the full test suite and report failures"
	if kinds(CheckUserTurns([]Entry{userTurn(half + half)}))[IssueSelfConcatenation] != 1 {
		t.Fatal("exact self-double not detected")
	}
}

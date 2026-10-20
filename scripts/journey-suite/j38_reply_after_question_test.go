// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func j38Turn(role, text string) any {
	return map[string]any{"role": role, "raw": map[string]any{"message": map[string]any{
		"role": role, "content": []any{map[string]any{"type": "text", "text": text}},
	}}}
}

const j38Asked = "Overseer here: what is the capital of France? Answer in one word, then carry on."

// The rows of the second full suite on 024bbdf1 (child store
// orch-midturn-58826): the answer and the final reply were folded into row 2,
// above the question. 🎯T1089 stores them after it, and J38 now refuses the
// folded shape instead of tolerating it.
func TestJ38RefusesTheFoldedSpecimen(t *testing.T) {
	folded := []any{
		j38Turn("user", "brief"),
		j38Turn("assistant", "I'll run the first `sleep 30` now. ParisSLEPTTWICE"),
		j38Turn("user", j38Asked),
		j38Turn("assistant", "GOAL_STATUS: complete"),
	}
	err := repliesAfterQuestion(folded, "PARIS", "SLEPTTWICE")
	if err == nil || !strings.Contains(err.Error(), "at or above the question") {
		t.Fatalf("the folded specimen passed J38's oracle: %v", err)
	}
}

func TestJ38AcceptsRepliesStoredAfterTheQuestion(t *testing.T) {
	ordered := []any{
		j38Turn("user", "brief"),
		j38Turn("assistant", "I'll run the first `sleep 30` now."),
		j38Turn("user", j38Asked),
		j38Turn("assistant", "Paris"),
		j38Turn("assistant", "sleptTwice"),
		j38Turn("assistant", "GOAL_STATUS: complete"),
	}
	if err := repliesAfterQuestion(ordered, "PARIS", "SLEPTTWICE"); err != nil {
		t.Fatalf("the ordered transcript failed: %v", err)
	}
	if err := repliesAfterQuestion(ordered[:4], "PARIS", "SLEPTTWICE"); err == nil {
		t.Fatal("a transcript without SLEPTTWICE yet passed")
	}
	if err := repliesAfterQuestion(append(ordered[:4:4], j38Turn("user", "SLEPTTWICE")), "SLEPTTWICE"); err == nil {
		t.Fatal("a user row is not the worker's reply")
	}
	if err := repliesAfterQuestion(ordered[:2], "PARIS"); err == nil {
		t.Fatal("a transcript with no question passed")
	}
}

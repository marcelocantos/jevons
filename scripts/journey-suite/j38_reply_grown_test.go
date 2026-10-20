// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func j38Turn(role, text string) any {
	return map[string]any{"role": role, "raw": map[string]any{"message": map[string]any{
		"role": role, "content": []any{map[string]any{"type": "text", "text": text}},
	}}}
}

// The rows of the second full suite on 024bbdf1 (child store orch-midturn-58826):
// the final answer was folded into row 2, which existed at the base snapshot.
func TestJ38ReplyGrownInARowThatExistedAtBase(t *testing.T) {
	base := []any{j38Turn("user", "brief"), j38Turn("assistant", "I'll run the first `sleep 30` now.")}
	now := []any{
		j38Turn("user", "brief"),
		j38Turn("assistant", "I'll run the first `sleep 30` now. ParisSLEPTTWICE"),
		j38Turn("user", "Overseer here: what is the capital of France?"),
		j38Turn("assistant", "GOAL_STATUS: complete"),
	}
	if !replyGrownSince(base, now, "SLEPTTWICE") {
		t.Fatal("the answer folded into a pre-existing row was missed")
	}
	if replyGrownSince(base, base, "SLEPTTWICE") {
		t.Fatal("an unchanged transcript reported a new answer")
	}
	stale := []any{j38Turn("assistant", "SLEPTTWICE from an earlier turn")}
	if replyGrownSince(stale, stale, "SLEPTTWICE") {
		t.Fatal("text present at base counted as a new answer")
	}
	if !replyGrownSince(stale, append(stale, j38Turn("assistant", "sleptTwice")), "SLEPTTWICE") {
		t.Fatal("a new row with the answer was missed")
	}
	if replyGrownSince(base, append(base, j38Turn("user", "SLEPTTWICE")), "SLEPTTWICE") {
		t.Fatal("a user row is not the worker's reply")
	}
}

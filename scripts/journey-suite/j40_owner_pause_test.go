// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// 🎯T806 journals the user bubble before delivery. J40 must observe the
// provider's answer, not mistake admission into the owner journal for one.
func TestJ40AssistantContainsIgnoresQueuedOwnerBubble(t *testing.T) {
	const token = "pause-hold-123"
	payload := map[string]any{"turns": []any{
		map[string]any{"role": "user", "raw": map[string]any{"message": map[string]any{"content": "Reply with exactly " + token}}},
		map[string]any{"role": "agent_note", "raw": map[string]any{"text": token}},
	}}
	if assistantContains(payload, token) {
		t.Fatal("queued owner bubble counted as provider answer")
	}
	payload["turns"] = append(payload["turns"].([]any),
		map[string]any{"role": "assistant", "raw": map[string]any{"message": map[string]any{"content": token}}})
	if !assistantContains(payload, token) {
		t.Fatal("authored answer not detected")
	}
}

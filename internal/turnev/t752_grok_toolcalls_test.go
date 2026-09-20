// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import "testing"

// 🎯T752 — Grok marks mid-turn tool use with a top-level tool_calls list, not
// with a content block. Until this decoder read that field, HasToolUse was
// false for every Grok assistant record, so nothing could tell a mid-turn
// record from the one that ends the turn — and a reader segmenting turns saw
// one turn spanning the whole session.
func TestT752GrokToolCallsAreToolUse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		line string
		want bool
	}{
		{"grok mid-turn", `{"type":"assistant","content":"Reading the file.","tool_calls":[{"id":"call-1"}]}`, true},
		{"grok terminal", `{"type":"assistant","content":"Done — here is the answer."}`, false},
		{"grok empty list", `{"type":"assistant","content":"hi","tool_calls":[]}`, false},
		{"grok null list", `{"type":"assistant","content":"hi","tool_calls":null}`, false},
		{"grok malformed list", `{"type":"assistant","content":"hi","tool_calls":"nope"}`, false},
		{"claude content block", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t"}]}}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, ok := Decode([]byte(c.line))
			if !ok {
				t.Fatalf("Decode(%s) not ok", c.line)
			}
			if rec.Kind != KindAssistant {
				t.Fatalf("Kind = %v, want assistant", rec.Kind)
			}
			if rec.HasToolUse != c.want {
				t.Fatalf("HasToolUse = %v, want %v", rec.HasToolUse, c.want)
			}
		})
	}
}

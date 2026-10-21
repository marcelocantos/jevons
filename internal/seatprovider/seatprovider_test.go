// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatprovider

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// Both vocabularies, in any case and with stray whitespace, resolve to one
// identity; the launch id is what claudia v0.58 routes each plan to.
func TestResolveEitherVocabulary(t *testing.T) {
	for _, tc := range []struct {
		names   []claudia.Provider
		plan    Plan
		launch  claudia.Provider
		sidecar bool
	}{
		{[]claudia.Provider{"claude", "anthropic", " Claude ", "ANTHROPIC"}, Claude, "anthropic", true},
		{[]claudia.Provider{"codex", "openai-codex", "Codex"}, Codex, "openai-codex", true},
		{[]claudia.Provider{"grok", "xai-oauth", "xai", "GROK"}, Grok, "xai-oauth", true},
		{[]claudia.Provider{"cursor", "Cursor"}, Cursor, "cursor", true},
		{[]claudia.Provider{"bedrock"}, Bedrock, "bedrock", false},
		{[]claudia.Provider{"ollama"}, Ollama, "ollama", false},
	} {
		for _, n := range tc.names {
			got := Resolve(n)
			want := Identity{Plan: tc.plan, Launch: tc.launch, Sidecar: tc.sidecar, Known: true}
			if got != want {
				t.Errorf("Resolve(%q) = %+v, want %+v", n, got, want)
			}
		}
		if !Same(tc.names[0], tc.names[len(tc.names)-1]) {
			t.Errorf("Same(%q, %q) = false", tc.names[0], tc.names[len(tc.names)-1])
		}
	}
	// Unset is not guessed: the caller owns what an empty provider means.
	if got := Resolve("  "); got != (Identity{}) {
		t.Errorf("Resolve(blank) = %+v, want the zero identity", got)
	}
	// An unknown id passes through, case-folded for the plan, as given for
	// the launch, and is not known or a sidecar seat.
	if got := Resolve(" Future-Backend "); got != (Identity{Plan: "future-backend", Launch: "Future-Backend"}) {
		t.Errorf("Resolve(unknown) = %+v", got)
	}
	if Same("grok", "claude") || Same("", "grok") {
		t.Error("distinct plans compare the same")
	}
}

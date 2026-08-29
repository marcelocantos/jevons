// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package modelladder

import "testing"

func TestNextWalksDownTheLadder(t *testing.T) {
	// The 2026-08-30 case: Fable's weekly ran out with Opus still funded.
	if got := Next("claude", "claude-fable-5"); got != "claude-opus-5" {
		t.Fatalf("fable falls back to %q, want claude-opus-5", got)
	}
	if got := Next("claude", "claude-opus-5"); got != "claude-sonnet-5" {
		t.Fatalf("opus falls back to %q", got)
	}
	if got := Next("grok", "grok-4.5"); got != "grok-4" {
		t.Fatalf("grok-4.5 falls back to %q", got)
	}
}

func TestNextRefusesToInventAModel(t *testing.T) {
	// Each of these must be "" — a guessed model id fails the launch and
	// costs the seat, which is worse than reporting no fallback.
	for _, c := range []struct{ provider, model string }{
		{"claude", "claude-haiku-4-5"}, // last rung
		{"grok", "grok-4"},             // last rung
		{"claude", "claude-imaginary"}, // unknown model
		{"bedrock", "anything"},        // no ladder
		{"", "claude-fable-5"},         // no provider
		{"claude", ""},                 // no model (provider default)
	} {
		if got := Next(c.provider, c.model); got != "" {
			t.Fatalf("Next(%q,%q)=%q, want empty", c.provider, c.model, got)
		}
	}
}

func TestKnownSeparatesNoLadderFromLastRung(t *testing.T) {
	if !Known("claude") || !Known("CLAUDE") {
		t.Fatal("claude has a ladder")
	}
	if Known("bedrock") || Known("") {
		t.Fatal("bedrock has no ladder")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// The 🎯T286 oracle. Every case here fails against the assembly this
// replaced (claudia.Agent.WaitForResponse): it newline-joins consecutive
// assistant events, and it latches "terminal seen" for the life of the
// call, so a terminal left over from an earlier turn arms the settle
// before this turn has said anything.

// testAssembler shortens both timers so the suite runs in milliseconds.
// The durations are the only thing scaled — the state machine under test
// is the production one.
func testAssembler(sep string) *replyAssembler {
	r := newReplyAssembler(sep)
	r.settleFor = 20 * time.Millisecond
	r.guardFor = 150 * time.Millisecond
	return r
}

func mustWait(t *testing.T, r *replyAssembler) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := r.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	return got
}

// delta is one token-level assistant event, as Grok's ACP stream emits.
func delta(text string) claudia.Event {
	return claudia.Event{Type: "assistant", Text: text}
}

// final is an assistant event carrying a terminal stop_reason.
func final(text string) claudia.Event {
	return claudia.Event{Type: "assistant", Text: text, StopReason: "end_turn"}
}

// A Grok reply arrives as token deltas that must be concatenated
// verbatim. This is the repro class from the mission: a short answer read
// as truncated because every consumer takes the first line.
func TestReplyAssemblerConcatenatesGrokDeltas(t *testing.T) {
	r := testAssembler(chunkSeparator(claudia.ProviderGrok))
	defer r.Close()
	r.Started()

	r.Observe(delta("BLUE"))
	r.Observe(delta("OTTER"))
	r.Observe(final("42"))

	got := mustWait(t, r)
	if got != "BLUEOTTER42" {
		t.Errorf("reply = %q, want %q", got, "BLUEOTTER42")
	}
	// Naming the old failures explicitly: a first-chunk-only assembler
	// returns "BLUE", and a newline-joining one returns "BLUE\nOTTER\n42".
	// Both must fail this test, or it is not an oracle for 🎯T286.
	if got == "BLUE" {
		t.Error("returned only the first chunk — the 🎯T286 truncation is back")
	}
	if got == "BLUE\nOTTER\n42" {
		t.Error("newline-joined Grok deltas — chunkSeparator is not being applied")
	}
}

// Block-shaped providers publish one event per content block, where the
// newline is the author's paragraphing and must be preserved.
func TestReplyAssemblerJoinsBlockProvidersWithNewline(t *testing.T) {
	r := testAssembler(chunkSeparator(claudia.ProviderClaude))
	defer r.Close()
	r.Started()

	r.Observe(delta("first block"))
	r.Observe(final("second block"))

	got := mustWait(t, r)
	if want := "first block\nsecond block"; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// Turn attribution, part one: this turn's first token cannot precede its
// own send, so anything seen before Started belongs to a turn still
// draining and must not contaminate the reply.
func TestReplyAssemblerDropsEventsBeforeStarted(t *testing.T) {
	r := testAssembler(chunkSeparator(claudia.ProviderGrok))
	defer r.Close()

	r.Observe(delta("PREVIOUS"))
	r.Observe(final("TURN"))

	r.Started()
	r.Observe(delta("OURS"))
	r.Observe(final("42"))

	got := mustWait(t, r)
	if want := "OURS42"; got != want {
		t.Errorf("reply = %q, want %q — earlier turn leaked in", got, want)
	}
}

// Turn attribution, part two: the 🎯T285 handover-seed window. The
// previous turn's terminal is fanned out AFTER our send, so it arrives
// with nothing to attribute it to. Resolving on it hands back the empty
// string (or, once our tokens start, the leading chunk alone). Assembly
// must instead treat a textless terminal as ambiguous and wait.
func TestReplyAssemblerStaleTerminalDoesNotTruncate(t *testing.T) {
	r := testAssembler(chunkSeparator(claudia.ProviderGrok))
	defer r.Close()
	r.Started()

	// Not ours: the seed turn's terminal, landing after our send.
	r.Observe(claudia.Event{Type: "assistant", StopReason: "end_turn"})

	// Ours, starting a beat later.
	r.Observe(delta("BLUE"))
	r.Observe(delta("OTTER"))
	r.Observe(final("42"))

	got := mustWait(t, r)
	if want := "BLUEOTTER42"; got != want {
		t.Errorf("reply = %q, want %q — resolved on another turn's terminal", got, want)
	}
}

// A slow first token must not be mistaken for a silent turn: tool-call
// progress is proof the turn is alive and re-arms the guard.
func TestReplyAssemblerToolProgressKeepsTurnAlive(t *testing.T) {
	r := testAssembler(chunkSeparator(claudia.ProviderGrok))
	defer r.Close()
	r.Started()

	r.Observe(claudia.Event{Type: "assistant", StopReason: "end_turn"})

	// Work, spanning longer than the guard would have allowed on its own.
	for range 3 {
		time.Sleep(60 * time.Millisecond)
		r.Observe(claudia.Event{Type: "progress", ProgressType: "tool_use"})
	}

	r.Observe(final("DONE"))

	got := mustWait(t, r)
	if got != "DONE" {
		t.Errorf("reply = %q, want %q — guard fired while the turn was working", got, "DONE")
	}
}

// A genuinely silent turn (tool calls only, no prose) still has to
// resolve rather than hang until the caller's timeout.
func TestReplyAssemblerSilentTurnResolvesEmpty(t *testing.T) {
	r := testAssembler(chunkSeparator(claudia.ProviderGrok))
	defer r.Close()
	r.Started()

	r.Observe(claudia.Event{Type: "assistant", StopReason: "end_turn"})

	if got := mustWait(t, r); got != "" {
		t.Errorf("reply = %q, want empty", got)
	}
}

// Trailing content blocks of the same message arrive after its
// stop_reason on some Claude Code versions, so the settle must extend
// rather than resolve on the first terminal.
func TestReplyAssemblerKeepsTrailingBlocksAfterTerminal(t *testing.T) {
	r := testAssembler(chunkSeparator(claudia.ProviderClaude))
	defer r.Close()
	r.Started()

	r.Observe(final("headline"))
	time.Sleep(5 * time.Millisecond)
	r.Observe(delta("tail"))

	got := mustWait(t, r)
	if want := "headline\ntail"; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

func TestChunkSeparator(t *testing.T) {
	for _, tc := range []struct {
		provider claudia.Provider
		want     string
	}{
		{claudia.ProviderGrok, ""},
		{claudia.ProviderClaude, "\n"},
		{claudia.ProviderCodex, "\n"},
		{"", "\n"},
	} {
		if got := chunkSeparator(tc.provider); got != tc.want {
			t.Errorf("chunkSeparator(%q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

// providerOf drives the separator choice, so a missing registry row must
// degrade to the block-shaped default rather than panic.
func TestProviderOfMissingRowIsSafe(t *testing.T) {
	var nilFleet *Claudia
	if got := nilFleet.providerOf("absent"); got != "" {
		t.Errorf("providerOf on nil fleet = %q, want empty", got)
	}

	f := &Claudia{}
	if got := f.providerOf("absent"); got != "" {
		t.Errorf("providerOf with no registry = %q, want empty", got)
	}
	if got := chunkSeparator(f.providerOf("absent")); got != "\n" {
		t.Errorf("separator for unknown provider = %q, want newline", got)
	}
}

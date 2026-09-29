// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T885: the overseer wire that enters a provider hard-block also clears it.
// A transport refusal enters; a transport frame is never evidence of
// recovery; the overseer's own served answer is.
func TestT885OverseerAnswerClearsHardBlock(t *testing.T) {
	s := New("test", t.TempDir())
	var failures, oks int
	s.SetProviderHardBlockHooks(func(agenterr.Class, string) { failures++ }, func() { oks++ })

	refusal := `provider refused the turn: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: refusal, IsError: true})
	if failures != 1 || oks != 0 {
		t.Fatalf("refusal: failures=%d oks=%d, want 1/0", failures, oks)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "session reset by peer", IsError: true})
	if oks != 0 {
		t.Fatalf("a transport frame cleared the hard-block (oks=%d)", oks)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "ge-po reported; nothing to relay.", StopReason: "end_turn"})
	if oks != 1 {
		t.Fatalf("the overseer's served answer did not clear the hard-block (oks=%d)", oks)
	}
}

// 🎯T897: a hard-block clears only on a SERVED turn — an assistant event
// whose StopReason is terminal (end_turn/stop_sequence/max_tokens). An
// authored text fragment with no terminal stop_reason (a tool_use pause, a
// mid-stream preview delta not yet sealed) must not clear the block: it is
// not evidence the provider finished accepting the turn, only that it
// produced some text along the way.
func TestT897HardBlockClearsOnlyOnServedTurn(t *testing.T) {
	s := New("test", t.TempDir())
	var failures, oks int
	s.SetProviderHardBlockHooks(func(agenterr.Class, string) { failures++ }, func() { oks++ })

	// Non-terminal authored text (e.g. a tool_use pause mid-turn): must not
	// clear the block.
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "let me check that", StopReason: "tool_use"})
	if oks != 0 {
		t.Fatalf("a non-terminal (tool_use) authored event cleared the hard-block (oks=%d)", oks)
	}
	// Authored text with StopReason unset entirely (provisional/interim
	// delta): must not clear the block either.
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "still working on it"})
	if oks != 0 {
		t.Fatalf("an authored event with no stop_reason cleared the hard-block (oks=%d)", oks)
	}
	// The terminal event of the same logical message: this is a served
	// turn and clears the block.
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "done", StopReason: "end_turn"})
	if oks != 1 {
		t.Fatalf("a terminal (end_turn) authored event did not clear the hard-block (oks=%d)", oks)
	}
	_ = failures
}

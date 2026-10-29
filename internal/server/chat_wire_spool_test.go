// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/spool"
)

func TestChatWireReadsSidecarSpool(t *testing.T) {
	lines := chatWireFromSpool([]spool.Record{
		{Type: "text", Text: wireOwnerReplyDecision("hello from sidecar"), Seat: "jevons-po"},
		{Type: "turn_end", Text: wireOwnerReplyDecision("done"), Seat: "jevons-po"},
	})
	if len(lines) == 0 {
		t.Fatal("chat-wire produced no lines from the spool")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "hello from sidecar") {
		t.Fatalf("normalizer missed spool text: %s", joined)
	}
}

func TestChatWireFromSpoolIncludesToolCall(t *testing.T) {
	lines := chatWireFromSpool([]spool.Record{
		{Type: "tool_call", CallID: "c0", Name: "Bash", Text: `{"cmd":"ls"}`},
	})
	if len(lines) != 1 {
		t.Fatalf("tool_call wire lines: got %d want 1", len(lines))
	}
}

func TestT870StopTokenIsNotOwnerVisible(t *testing.T) {
	lines := chatWireFromSpool([]spool.Record{
		{Type: "text", Text: "<|eos|>", Seat: "jevons"},
		{Type: "text", Text: wireOwnerReplyDecision("I'll inspect "), Seat: "jevons"},
	})
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "<|eos|>") {
		t.Fatalf("owner wire kept the stop token: %s", joined)
	}
	if !strings.Contains(joined, "I'll inspect ") {
		t.Fatalf("owner wire dropped the sentence: %s", joined)
	}
}

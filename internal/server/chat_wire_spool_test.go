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
		{Type: "text", Text: "hello from sidecar", Seat: "jevons-po"},
		{Type: "turn_end", Text: "done", Seat: "jevons-po"},
	})
	if len(lines) == 0 {
		t.Fatal("chat-wire produced no lines from the spool")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "hello from sidecar") {
		t.Fatalf("normalizer missed spool text: %s", joined)
	}
}

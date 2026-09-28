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
// recovery; the overseer's own answer is.
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
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "ge-po reported; nothing to relay."})
	if oks != 1 {
		t.Fatalf("the overseer answering did not clear the hard-block (oks=%d)", oks)
	}
}

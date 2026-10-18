// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/converge"
)

// A raced drain records the owner request id and does not see the
// receipt yet. Once that same id is Started, the re-inject step
// re-queries it and does not mint a second prompt. The send-landed
// gap stays open until the next observation, which is the pass that
// sees the settled receipt.
func TestT1074OwnerReceiptRequerySkipsReinjection(t *testing.T) {
	var sent []string
	s, _, clk := ownerHealthServer(t, func(text string) error {
		sent = append(sent, text)
		return nil
	})
	s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if q.RequestID != "om-open" {
			return durableOwnerReceipt(q, claudiawire.ReceiptUnknown, ""), nil
		}
		return durableOwnerReceipt(q, claudiawire.ReceiptStarted, ""), nil
	})

	const text = "what is the frontier?"
	echo := chatUserEcho(text)
	s.NoteOwnerSend(text, echo)
	s.BroadcastChat(echo)
	s.noteOwnerRequest("om-open")
	s.mu.Lock()
	s.notifyQueue = nil
	s.notifyInFlight = nil
	s.mu.Unlock()

	s.ReconcileOwnerHealth(clk.Advance(10 * time.Second))
	if len(sent) != 0 {
		t.Fatalf("re-injected an owner turn whose receipt had started: %v", sent)
	}
	if _, open := gapFor(s, converge.OwnerDimSendLanded); !open {
		t.Fatal("send-landed gap closed inside the step that re-queried the receipt")
	}

	s.ReconcileOwnerHealth(clk.Advance(10 * time.Second))
	if _, open := gapFor(s, converge.OwnerDimSendLanded); open {
		t.Fatal("send-landed gap still standing after the started receipt was observed")
	}
	if len(sent) != 0 {
		t.Fatalf("second reconcile minted a prompt: %v", sent)
	}
}

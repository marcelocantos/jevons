// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1295. J3 on codex, 2026-10-29 (isolate jevons-journey-2027596206): the
// owner cancelled a turn held in a Bash call and sent a replacement. The
// server settled the cancel at once; the provider's abort terminal for the
// cancelled request reached the event path only after the replacement had
// been handed over. That terminal ended the replacement's owner turn, so the
// replacement streamed as a non-owner turn and its reply (27 deltas, the
// exact token) was suppressed: the owner saw two empty bubbles.
func TestT1295LateAbortOfTheCancelledTurnDoesNotEndTheReplacement(t *testing.T) {
	s, _, sent := replacedOverseerServer(t)

	if err := s.SendToOverseerAs(userTurnPrefix+"Run the held command", "om-long"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "receipt", RequestID: "om-long",
		Receipt: &claudia.RequestReceipt{RequestID: "om-long", State: claudia.ReceiptStarted}})

	// The owner cancels; the server settles before the provider says so.
	s.settleOwnerCancel()

	// The replacement is handed over.
	const replacement = userTurnPrefix + "Reply with exactly: ok-token"
	if err := s.SendToOverseerAs(replacement, "om-replacement"); err != nil {
		t.Fatal(err)
	}
	if len(*sent) != 2 || (*sent)[1] != replacement {
		t.Fatalf("handoffs = %v, want the replacement second", *sent)
	}

	// Only now does the cancelled turn's abort terminal arrive.
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn", Reason: claudia.ReasonAborted,
		TurnID: "t-long", RequestID: "om-long"})
	s.mu.Lock()
	waiting, ownerTurn := s.waiting, s.overseerOwnerTurn
	s.mu.Unlock()
	if !waiting || !ownerTurn {
		t.Fatalf("after the cancelled turn's late abort: waiting=%v owner_turn=%v, want the replacement's owner turn still held", waiting, ownerTurn)
	}

	// The replacement runs and answers; its own terminal ends the owner turn.
	s.DeliverOverseerEvent(claudia.Event{Type: "receipt", RequestID: "om-replacement",
		Receipt: &claudia.RequestReceipt{RequestID: "om-replacement", State: claudia.ReceiptStarted}})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "ok-token", TurnID: "t-replacement", RequestID: "om-replacement"})
	s.mu.Lock()
	silent := s.overseerStreamSilent
	s.mu.Unlock()
	if silent {
		t.Fatal("the replacement's reply was classified silent: it streamed as a non-owner turn")
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn", TurnID: "t-replacement", RequestID: "om-replacement"})
	s.mu.Lock()
	ownerTurn = s.overseerOwnerTurn
	s.mu.Unlock()
	if ownerTurn {
		t.Fatal("owner turn still held after the replacement's own terminal")
	}
}

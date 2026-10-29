// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T1064 scout oracle, second hypothesis: sendToOverseerAsOwner's two
// writes for one owner submit — BroadcastChat(echo) immediately, then
// SendToOverseerAs delivering the turn to the live overseer process — are
// mutually exclusive ONLY if the provider never echoes the delivered prompt
// back as its own "user" event. journalAgentEvent's provider-echo dedupe
// latch (agent_chatlog.go) is explicitly skipped for the overseer
// (isOverseerAgent(name) early-return), so if the overseer's own event
// stream independently re-emits the delivered turn as a "user" event
// through DeliverOverseerEvent — the live path this exact daemon uses for
// the overseer (chat_roundtrip_test.go, t1091_owner_turn_test.go) — the only
// thing standing between one bubble and two is chatWireLine's
// isOwnerNotifyText dedup (T382), not a second independent latch.
//
// This test drives exactly that: one sendToOverseerAsOwner call, then one
// simulated provider echo of the SAME delivered prompt through
// DeliverOverseerEvent, and counts how many user-typed broadcast lines
// reach the owner's chat listeners.
func TestT1064OneOwnerSendOneOverseerEchoCountsUserLines(t *testing.T) {
	s, live, sent := replacedOverseerServer(t)
	s.AttachOverseer(live)
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	s.SetOverseerReattachWait(0)

	ch := make(chan string, 16)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	const body = "Why is UI not rendering markdown in requests?"
	if err := s.sendToOverseerAsOwner(body); err != nil {
		t.Fatalf("sendToOverseerAsOwner: %v", err)
	}
	if len(*sent) != 1 {
		t.Fatalf("overseer handoffs = %v, want exactly 1", *sent)
	}
	delivered := (*sent)[0]

	// The provider (e.g. Claude) echoes the delivered prompt back as its
	// own "user" event, exactly as the real ACP wire does — this is the
	// claudeRaw shape from TestOwnerTurnPaintedOnceUnderEveryProvider.
	claudeRaw, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": delivered,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "user", Raw: claudeRaw})

	// Drain everything the one owner send produced.
	var lines []string
	deadline := time.After(200 * time.Millisecond)
collect:
	for {
		select {
		case line := <-ch:
			lines = append(lines, line)
		case <-deadline:
			break collect
		}
	}

	userBubbles := 0
	for _, line := range lines {
		if paintsOwnerBubble(t, line) {
			userBubbles++
		}
	}
	if userBubbles != 1 {
		t.Fatalf("one owner send + one provider echo produced %d user-typed bubbles (lines=%v), want exactly 1 — T1064 reproduced if >1", userBubbles, lines)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"testing"
	"time"
)

// SpentTokens is the 🎯T359 capacity lever: it must total every token class
// in the window and, like SpentUSD, refuse to let a poisoned negative row
// deflate the count.
func TestSpentTokensTotalsWindowAndIgnoresNegatives(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()

	inWindow := &Event{
		Timestamp: testNow.Add(-time.Minute),
		SessionID: "w1", Worker: "fleet-w", Model: "claude-opus-4-8",
		Usage:     Usage{Input: 10, Output: 5, CacheCreate: 3, CacheRead: 2},
		RequestID: "r1",
	}
	poisoned := &Event{
		Timestamp: testNow.Add(-time.Minute),
		SessionID: "w2", Model: "claude-opus-4-8",
		Usage:     Usage{Input: -1000, Output: 1},
		RequestID: "r2",
	}
	outOfWindow := &Event{
		Timestamp: testNow.Add(-48 * time.Hour),
		SessionID: "w3", Model: "claude-opus-4-8",
		Usage:     Usage{Output: 999},
		RequestID: "r3",
	}
	if _, err := s.InsertEvents([]*Event{inWindow, poisoned, outOfWindow}); err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}

	got, err := s.SpentTokens(testNow.Add(-time.Hour), testNow.Add(time.Hour))
	if err != nil {
		t.Fatalf("SpentTokens: %v", err)
	}
	if got != 21 {
		t.Fatalf("SpentTokens = %d, want 21 (20 in-window + 1 from the clamped row)", got)
	}
}

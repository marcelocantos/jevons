// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatstate

import (
	"testing"
	"time"
)

// Queue stoppage is one claim. Depth, the head's enqueue time, and the block
// reason move together, go stale together, and do not survive a new session.
func TestT7665QueueStoppageDecaysAndRemintsWithDepth(t *testing.T) {
	a, c := newAt(at("2026-10-19T00:00:00Z"), time.Minute)
	oldest := c.now().Add(-2 * time.Hour)
	blocked := "in flight"
	a.FromQueue("seat", QueueReport{Depth: 2, Oldest: oldest, Block: &blocked}, c.now())
	s, ok := a.Get("seat")
	if !ok || s.QueueDepth != 2 || !s.QueueOldest.Equal(oldest) || s.QueueBlock == nil || *s.QueueBlock != "in flight" {
		t.Fatalf("stoppage = depth %d oldest %v block %v ok=%v", s.QueueDepth, s.QueueOldest, s.QueueBlock, ok)
	}
	if got := s.QueueOldestAge(c.now()); got != 2*time.Hour {
		t.Fatalf("age = %s, want 2h", got)
	}

	// A report that names a depth and not a block does not erase the block.
	c.tick(time.Second)
	a.FromQueue("seat", QueueReport{Depth: 2}, c.now())
	s, _ = a.Get("seat")
	if s.QueueDepth != 2 || s.QueueBlock == nil || *s.QueueBlock != "in flight" || !s.QueueOldest.Equal(oldest) {
		t.Fatalf("depth-only report drifted the stoppage: depth %d oldest %v block %v", s.QueueDepth, s.QueueOldest, s.QueueBlock)
	}

	// An empty block string is a claim that the queue is not blocked.
	c.tick(time.Second)
	open := ""
	head := c.now().Add(-time.Minute)
	a.FromQueue("seat", QueueReport{Depth: 1, Oldest: head, Block: &open}, c.now())
	s, _ = a.Get("seat")
	if s.QueueDepth != 1 || !s.QueueOldest.Equal(head) || s.QueueBlock == nil || *s.QueueBlock != "" {
		t.Fatalf("empty block was not a claim: depth %d oldest %v block %v", s.QueueDepth, s.QueueOldest, s.QueueBlock)
	}
	if s.QueueOldestAge(c.now()) != time.Minute {
		t.Fatalf("age = %s, want 1m", s.QueueOldestAge(c.now()))
	}

	// A failed read must not wipe the claim.
	a.FromQueue("seat", QueueReport{Depth: -1, Block: &blocked}, c.now())
	s, _ = a.Get("seat")
	if s.QueueDepth != 1 || s.QueueBlock == nil || *s.QueueBlock != "" {
		t.Fatalf("failed read wiped the queue: depth %d block %v", s.QueueDepth, s.QueueBlock)
	}

	// The queue signal goes stale as one fact, not depth first and the
	// reason later.
	c.tick(2 * time.Minute)
	s, _ = a.Get("seat")
	if s.QueueDepth != QueueUnknown || !s.QueueOldest.IsZero() || s.QueueBlock != nil {
		t.Fatalf("stale stoppage lingered: depth %d oldest %v block %v", s.QueueDepth, s.QueueOldest, s.QueueBlock)
	}

	a.FromQueue("seat", QueueReport{Depth: 2, Oldest: oldest, Block: &blocked}, c.now())
	c.tick(time.Second)
	a.Observe(Observation{Name: "seat", SessionID: "new", Source: "registry", QueueDepth: QueueUnknown, At: c.now()})
	s, _ = a.Get("seat")
	if s.SessionID != "new" || s.QueueDepth != QueueUnknown || !s.QueueOldest.IsZero() || s.QueueBlock != nil {
		t.Fatalf("remint kept the queue: session %q depth %d oldest %v block %v", s.SessionID, s.QueueDepth, s.QueueOldest, s.QueueBlock)
	}
}

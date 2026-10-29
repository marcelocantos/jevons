// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"strings"
	"testing"
)

// 🎯T1252: a small backlog (below DigestThreshold) used to drain one entry
// per terminal stop — the rest waited for the NEXT time the seat went idle,
// which could be a full long turn away. ClaimAll merges it into one turn,
// each message delivered in full and attributed, in order.
func TestT1252ClaimAllMergesSmallBacklogIntoOneTurn(t *testing.T) {
	s := NewStore(t.TempDir())
	t774Queue(t, s, 3)

	// Below DigestThreshold: ClaimDigest must not touch it.
	if _, ok, err := s.ClaimDigest("a"); err != nil || ok {
		t.Fatalf("small backlog claimed by ClaimDigest: ok=%v err=%v", ok, err)
	}

	merged, ok, err := s.ClaimAll("a")
	if err != nil || !ok {
		t.Fatalf("ClaimAll: ok=%v err=%v", ok, err)
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(merged.Text, "m"+string(rune('0'+i))+": body") {
			t.Fatalf("merged text missing message %d, in order/attributed: %q", i, merged.Text)
		}
	}
	// Nothing collapsed away: every original is still named by entry id.
	if strings.Count(merged.Text, "entry id=") != 3 {
		t.Fatalf("expected 3 attributed entries, got: %q", merged.Text)
	}

	// The whole backlog was claimed as ONE entry: nothing left pending behind it.
	if _, ok2, _ := s.ClaimFront("a"); ok2 {
		t.Fatalf("a second entry remained pending after ClaimAll merged the backlog")
	}
}

// A single pending entry is left for ClaimFront: nothing to merge.
func TestT1252ClaimAllNoOpOnSingleEntry(t *testing.T) {
	s := NewStore(t.TempDir())
	t774Queue(t, s, 1)
	if _, ok, err := s.ClaimAll("a"); err != nil || ok {
		t.Fatalf("single entry claimed by ClaimAll: ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.ClaimFront("a"); err != nil || !ok {
		t.Fatalf("single entry not claimable by ClaimFront: ok=%v err=%v", ok, err)
	}
}

// Above DigestThreshold, ClaimAll defers to ClaimDigest's collapsing shape —
// drainAgentSendQueueOnce tries ClaimDigest first, so ClaimAll never sees
// that backlog in production, but ClaimAll itself must still refuse it.
func TestT1252ClaimAllDefersAboveThreshold(t *testing.T) {
	s := NewStore(t.TempDir())
	t774Queue(t, s, DigestThreshold+1)
	if _, ok, err := s.ClaimAll("a"); err != nil || ok {
		t.Fatalf("over-threshold backlog claimed by ClaimAll: ok=%v err=%v", ok, err)
	}
}

// An entry this store owns in flight blocks the whole queue — ClaimAll
// refuses exactly like ClaimFront and ClaimDigest do, touching nothing.
func TestT1252ClaimAllBlockedBehindOwnedInFlightEntry(t *testing.T) {
	s := NewStore(t.TempDir())
	t774Queue(t, s, 3)
	if _, ok, err := s.ClaimFront("a"); err != nil || !ok {
		t.Fatalf("ClaimFront held: ok=%v err=%v", ok, err)
	}
	// Two remain Pending behind the held (Attempting) entry this store owns.
	if _, ok, err := s.ClaimAll("a"); err != nil || ok {
		t.Fatalf("ClaimAll claimed behind an owned in-flight entry: ok=%v err=%v", ok, err)
	}
}

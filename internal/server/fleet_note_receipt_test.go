// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
)

// fleetReceiptServer is an overseer with a registry session whose notify
// drain writes through the request-id seam. publish makes the receipt for a
// handed-off request visible to the receipt read; until then the replica has
// no row, as when Claudia's seat pump publishes after the send returns.
func fleetReceiptServer(t *testing.T) (s *Server, ids func() []string, publish func(string), queries func() int) {
	t.Helper()
	s, _ = drainReceiptServer(t)
	installOwnerReceiptSession(t, s)
	var mu sync.Mutex
	published := map[string]bool{}
	n := 0
	var sent []string
	s.overseerSendSeam = func(_, id string) error {
		mu.Lock()
		sent = append(sent, id)
		mu.Unlock()
		return nil
	}
	s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if q.Seat != "jevons" || q.SessionID != "gen-overseer" {
			t.Errorf("receipt query = %+v", q)
		}
		if !published[q.RequestID] {
			return nil, fmt.Errorf("replicated receipt absent: %w", claudiawire.ErrReceiptAbsent)
		}
		return durableOwnerReceipt(q, claudiawire.ReceiptCompleted, "turn completed"), nil
	})
	ids = func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sent...)
	}
	publish = func(id string) {
		mu.Lock()
		published[id] = true
		mu.Unlock()
	}
	queries = func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
	return s, ids, publish, queries
}

// An agent note to an idle overseer (a 🎯T902 mid-turn answer relay, a worker
// report) goes out in its own drain with an fn- request id. Its receipt,
// published after the send returns, settles the handoff. Before this, the
// fleet batch's receipt was never read and every such send stayed
// delivered_unconfirmed.
func TestFleetNoteToIdleOverseerSettlesFromItsBatchReceipt(t *testing.T) {
	s, ids, publish, _ := fleetReceiptServer(t)
	note := "[Agent orch-midturn answered your message mid-turn, and is still working]\nParis"
	go func() {
		// The seat pump publishes after the handoff returns.
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if got := ids(); len(got) == 1 {
				time.Sleep(50 * time.Millisecond)
				publish(got[0])
				return
			}
		}
	}()
	settled, err := s.DeliverToOverseerAsReceipted(note, sendOriginAgent)
	if err != nil {
		t.Fatal(err)
	}
	got := ids()
	if len(got) != 1 || !strings.HasPrefix(got[0], "fn-") {
		t.Fatalf("handoff request ids = %v, want one fn- id", got)
	}
	if !settled {
		t.Fatal("the fleet note's receipt settled after the send returned, but the handoff was reported unconfirmed")
	}
}

// A receipt that never arrives leaves the note unconfirmed, and the caller is
// released at the caller bound rather than held for the whole read.
func TestFleetNoteWithoutReceiptStaysUnconfirmedWithinTheCallerBound(t *testing.T) {
	s, ids, _, _ := fleetReceiptServer(t)
	start := time.Now()
	settled, err := s.DeliverToOverseerAsReceipted("[event: worker-idle] jv-x went idle", sendOriginAgent)
	if err != nil {
		t.Fatal(err)
	}
	if settled {
		t.Fatal("no receipt was published, yet the handoff settled")
	}
	if len(ids()) != 1 {
		t.Fatalf("handoffs = %v", ids())
	}
	if took := time.Since(start); took > fleetReceiptCallerWait+2*time.Second {
		t.Fatalf("caller held %s, bound is %s", took, fleetReceiptCallerWait)
	}
}

// A note queued behind a busy overseer was not handed off by this call, so
// this call does not wait for or claim a receipt.
func TestFleetNoteQueuedBehindBusyOverseerIsNotClaimed(t *testing.T) {
	s, ids, _, queries := fleetReceiptServer(t)
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	settled, err := s.DeliverToOverseerAsReceipted("[event: worker-idle] jv-y went idle", sendOriginAgent)
	if err != nil {
		t.Fatal(err)
	}
	if settled || len(ids()) != 0 || queries() != 0 {
		t.Fatalf("queued note: settled=%v handoffs=%v queries=%d", settled, ids(), queries())
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !jevons_claudia_next

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudiawire"
)

const receiptLaneWhen = "2026-10-19T00:00:00.000000000Z"

// receiptLane keeps the default build compiling. The map is filled only by
// publish. The query does not choose a state from the request id. Acceptance
// is the jevons_claudia_next lane, which reads the replica.
type receiptLane struct {
	publish func(seat, session, id string, state claudiawire.ReceiptState, cause claudiawire.ReceiptCause, reason string, turns []string)
}

func bindReceiptLane(t *testing.T, s *Server, dir string) receiptLane {
	t.Helper()
	_ = dir
	var mu sync.Mutex
	stored := map[string]*claudiawire.RequestReceipt{}
	key := func(seat, session, id string) string {
		return seat + "\x00" + session + "\x00" + id
	}
	s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		mu.Lock()
		defer mu.Unlock()
		got := stored[key(q.Seat, q.SessionID, q.RequestID)]
		if got == nil {
			return nil, fmt.Errorf("replicated receipt absent")
		}
		copyReceipt := *got
		copyReceipt.TurnIDs = append([]string{}, got.TurnIDs...)
		return &copyReceipt, nil
	})
	_, err := s.LookupHeldReceipt(context.Background(), claudiawire.ReceiptQuery{Seat: "lane", SessionID: "lane", RequestID: "absent"})
	if err == nil || !strings.Contains(err.Error(), "replicated receipt absent") {
		t.Fatalf("receipt query was not installed: %v", err)
	}
	return receiptLane{publish: func(seat, session, id string, state claudiawire.ReceiptState, cause claudiawire.ReceiptCause, reason string, turns []string) {
		mu.Lock()
		defer mu.Unlock()
		stored[key(seat, session, id)] = &claudiawire.RequestReceipt{
			Seat: seat, SessionID: session, RequestID: id,
			State: state, Cause: cause, Reason: reason,
			AttemptID:  "attempt-" + id,
			TurnIDs:    append([]string{}, turns...),
			AcceptedAt: receiptLaneWhen,
			UpdatedAt:  receiptLaneWhen,
		}
	}}
}

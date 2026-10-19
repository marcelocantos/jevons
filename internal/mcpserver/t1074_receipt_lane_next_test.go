// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/brokerfixture/controllane"
	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/cost"
)

const receiptLaneWhen = "2026-10-19T00:00:00.000000000Z"

// receiptLane is the receipt publish the broker's seat pump performs, in the
// broker's own process and file. The read is the replica query
// startCostGuard installs.
type receiptLane struct {
	publish func(seat, session, id string, state claudiawire.ReceiptState, cause claudiawire.ReceiptCause, reason string, turns []string)
}

// bindReceiptLane starts a broker process, opens Jevons' replica linked to
// it, and installs ReplicatedReceipt. It does not start the collector loop.
func bindReceiptLane(t *testing.T, s *Server, dir string) receiptLane {
	t.Helper()
	store, err := cost.OpenStore(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	lane := controllane.Start(t, dir, store)
	collector := lane.Collector
	collector.BindSendReceiptQuery(s.SetSendReceiptQuery)
	t.Log("production replica receipt query")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = s.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{Seat: "lane", SessionID: "lane", RequestID: "absent"})
	if err == nil || !strings.Contains(err.Error(), "replicated receipt absent") {
		t.Fatalf("production replica receipt query was not installed: %v", err)
	}
	return receiptLane{publish: func(seat, session, id string, state claudiawire.ReceiptState, cause claudiawire.ReceiptCause, reason string, turns []string) {
		lane.PublishReceipt(claudia.RequestReceipt{
			Seat: seat, SessionID: session, RequestID: id,
			State: state, Cause: cause, Reason: reason,
			AttemptID:  "attempt-" + id,
			TurnIDs:    append([]string{}, turns...),
			AcceptedAt: receiptLaneWhen,
			UpdatedAt:  receiptLaneWhen,
		})
	}}
}

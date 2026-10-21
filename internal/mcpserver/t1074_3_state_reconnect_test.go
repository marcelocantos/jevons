// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

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

// TestT1074StartedAndCancelledReceiptsSurvivePlaneReconnect publishes a
// started receipt and a cancelled receipt, closes the control plane, and
// reads both back through the replica query startCostGuard installs. A real
// broker process publishes them in its own file; they reach Jevons' replica
// over the control socket.
func TestT1074StartedAndCancelledReceiptsSurvivePlaneReconnect(t *testing.T) {
	const (
		seat    = "jv-t1074-3-states"
		session = "gen-t1074-3-states"
		when    = "2026-10-19T00:00:00.000000000Z"
	)
	dir := t.TempDir()
	ctx := context.Background()
	store, err := cost.OpenStore(filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	s := &Server{}
	lane := controllane.Start(t, dir, store)
	bind := func(collector *cost.Collector) {
		t.Helper()
		collector.BindSendReceiptQuery(s.SetSendReceiptQuery)
		t.Log("production replica receipt query")
	}
	bind(lane.Collector)
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = s.LookupHeldReceipt(probeCtx, claudiawire.ReceiptQuery{Seat: "lane", SessionID: "lane", RequestID: "absent"})
	if err == nil || !strings.Contains(err.Error(), "replicated receipt absent") {
		t.Fatalf("production replica receipt query was not installed: %v", err)
	}

	startedID := "host-started"
	cancelledID := "host-cancelled"
	unknownID := "host-unknown"
	lane.PublishReceipt(claudia.RequestReceipt{
		RequestID: startedID, SessionID: session, Seat: seat,
		State: claudia.ReceiptStarted, AttemptID: "attempt-" + startedID,
		TurnIDs: []string{"turn-started"}, Cause: "", Reason: "still running",
		AcceptedAt: when, UpdatedAt: when,
	})
	lane.PublishReceipt(claudia.RequestReceipt{
		RequestID: cancelledID, SessionID: session, Seat: seat,
		State: claudia.ReceiptCancelled, AttemptID: "attempt-" + cancelledID,
		TurnIDs: []string{"turn-cancelled"}, Cause: claudia.ReceiptCauseCancellation,
		Reason:     "broker shutdown",
		AcceptedAt: when, UpdatedAt: when,
	})
	lane.PublishReceipt(claudia.RequestReceipt{
		RequestID: unknownID, SessionID: session, Seat: seat,
		State: claudia.ReceiptUnknown, AttemptID: "attempt-" + unknownID,
		TurnIDs: []string{}, Cause: "", Reason: "not_found",
		AcceptedAt: when, UpdatedAt: when,
	})

	read := func(id string, state claudia.ReceiptState, cause claudia.ReceiptCause, reason, turn string) *claudiawire.RequestReceipt {
		t.Helper()
		got, err := s.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{
			Seat: seat, SessionID: session, RequestID: id,
		})
		if err != nil {
			t.Fatalf("lookup %s: %v", id, err)
		}
		if got.RequestID != id || got.SessionID != session || got.Seat != seat ||
			got.State != state || got.Cause != cause || got.Reason != reason ||
			got.AttemptID != "attempt-"+id || len(got.TurnIDs) != 1 || got.TurnIDs[0] != turn ||
			got.AcceptedAt != when || got.UpdatedAt != when {
			t.Fatalf("lookup %s = %+v", id, got)
		}
		return got
	}
	beforeStarted := read(startedID, claudia.ReceiptStarted, "", "still running", "turn-started")
	beforeCancelled := read(cancelledID, claudia.ReceiptCancelled, claudia.ReceiptCauseCancellation, "broker shutdown", "turn-cancelled")
	beforeUnknown, err := s.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{Seat: seat, SessionID: session, RequestID: unknownID})
	if err != nil {
		t.Fatal(err)
	}
	if beforeUnknown.RequestID != unknownID || beforeUnknown.State != claudia.ReceiptUnknown || beforeUnknown.Cause != "" || beforeUnknown.Reason != "not_found" || beforeUnknown.AttemptID != "attempt-"+unknownID || beforeUnknown.Seat != seat || beforeUnknown.SessionID != session {
		t.Fatalf("unknown receipt = %+v", beforeUnknown)
	}
	if beforeStarted.AttemptID == beforeCancelled.AttemptID || beforeStarted.RequestID == beforeCancelled.RequestID || beforeUnknown.RequestID == beforeStarted.RequestID {
		t.Fatalf("states share an identity: %+v %+v %+v", beforeStarted, beforeCancelled, beforeUnknown)
	}

	// A Jevons restart closes the replica and opens it again. The rows
	// already stored are what the reopened query returns.
	bind(lane.Reopen())
	afterStarted := read(startedID, claudia.ReceiptStarted, "", "still running", "turn-started")
	afterCancelled := read(cancelledID, claudia.ReceiptCancelled, claudia.ReceiptCauseCancellation, "broker shutdown", "turn-cancelled")
	afterUnknown, err := s.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{Seat: seat, SessionID: session, RequestID: unknownID})
	if err != nil {
		t.Fatal(err)
	}
	if afterStarted.UpdatedAt != beforeStarted.UpdatedAt || afterStarted.AttemptID != beforeStarted.AttemptID || afterStarted.State != beforeStarted.State {
		t.Fatalf("reconnect changed the started receipt: %+v then %+v", beforeStarted, afterStarted)
	}
	if afterCancelled.UpdatedAt != beforeCancelled.UpdatedAt || afterCancelled.AttemptID != beforeCancelled.AttemptID || afterCancelled.Cause != beforeCancelled.Cause || afterCancelled.State != beforeCancelled.State {
		t.Fatalf("reconnect changed the cancelled receipt: %+v then %+v", beforeCancelled, afterCancelled)
	}
	if afterUnknown.State != claudia.ReceiptUnknown || afterUnknown.RequestID != unknownID || afterUnknown.Reason != "not_found" || afterUnknown.Cause != "" || afterUnknown.AttemptID != beforeUnknown.AttemptID || afterUnknown.UpdatedAt != beforeUnknown.UpdatedAt {
		t.Fatalf("reconnect changed the unknown receipt: %+v then %+v", beforeUnknown, afterUnknown)
	}
	swapped, err := s.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{Seat: seat, SessionID: session, RequestID: startedID})
	if err != nil || swapped.State != claudia.ReceiptStarted || swapped.RequestID != startedID {
		t.Fatalf("started id read the cancelled receipt: %+v %v", swapped, err)
	}
}

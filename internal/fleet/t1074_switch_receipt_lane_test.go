// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

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

const switchReceiptWhen = "2026-10-19T00:00:00.000000000Z"

// switchReceiptLane reads receipts through the query startCostGuard installs,
// over Jevons' replica linked to a real broker process, which publishes them
// in its own file. live is true only on this build. The collector loop stays
// stopped.
type switchReceiptLane struct {
	live    bool
	publish func(seat, session, id string, state claudiawire.ReceiptState, cause claudiawire.ReceiptCause, reason string, turns []string)
	read    func(context.Context, string, string, string) (*claudiawire.RequestReceipt, error)
}

func openSwitchReceiptLane(t *testing.T, dir string) switchReceiptLane {
	t.Helper()
	store, err := cost.OpenStore(filepath.Join(dir, "switch-usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	lane := controllane.Start(t, dir, store)
	collector := lane.Collector
	var query func(context.Context, claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error)
	collector.BindSendReceiptQuery(func(fn func(context.Context, claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error)) {
		query = fn
	})
	if query == nil {
		t.Fatal("production replica receipt query was not installed")
	}
	t.Log("production replica receipt query")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = query(ctx, claudiawire.ReceiptQuery{Seat: "lane", SessionID: "lane", RequestID: "absent"})
	if err == nil || !strings.Contains(err.Error(), "replicated receipt absent") {
		t.Fatalf("production replica receipt query was not installed: %v", err)
	}
	return switchReceiptLane{
		live: true,
		publish: func(seat, session, id string, state claudiawire.ReceiptState, cause claudiawire.ReceiptCause, reason string, turns []string) {
			lane.PublishReceipt(claudia.RequestReceipt{
				Seat: seat, SessionID: session, RequestID: id,
				State: state, Cause: cause, Reason: reason,
				AttemptID:  "attempt-" + id,
				TurnIDs:    append([]string{}, turns...),
				AcceptedAt: switchReceiptWhen,
				UpdatedAt:  switchReceiptWhen,
			})
		},
		read: func(ctx context.Context, seat, session, id string) (*claudiawire.RequestReceipt, error) {
			return query(ctx, claudiawire.ReceiptQuery{Seat: seat, SessionID: session, RequestID: id})
		},
	}
}

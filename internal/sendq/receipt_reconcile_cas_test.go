// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"testing"
	"time"
)

func TestReceiptReconcileRejectsAttemptRequeuedWhileQuerying(t *testing.T) {
	q := NewStore(t.TempDir())
	e, _, _, err := q.AppendAndClaimWithRequestID("a", "payload", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Resolve("a", e, Unverified, "ack lost"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Reconcile("a", e.ID, e.AttemptID, ReconcileRequeue, "owner", "proved absent", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Reconcile("a", e.ID, e.AttemptID, ReconcileConfirmed, "daemon:receipt", "late query result", time.Now()); err == nil {
		t.Fatal("stale receipt removed requeued pending obligation")
	}
	entries, err := q.Snapshot("a")
	if err != nil || len(entries) != 1 || entries[0].State != Pending || entries[0].AttemptID != "" {
		t.Fatalf("pending entry changed: %+v %v", entries, err)
	}
	// Deliberate operator disposition of a pending entry still needs no
	// attempt token; it is not a late result from an earlier submission.
	if _, err := q.Reconcile("a", e.ID, "", ReconcileDrop, "owner", "no longer needed", time.Now()); err != nil {
		t.Fatal(err)
	}
}

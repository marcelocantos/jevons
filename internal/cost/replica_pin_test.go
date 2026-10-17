// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !jevons_claudia_next

package cost

import (
	"context"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudiawire"
)

// The published pin has no control plane. OpenReplica refuses, a collector
// the daemon built with that nil source does not ingest, and a non-nil
// stand-in is still a refusal. replica.go is the real source and builds
// only with jevons_claudia_next.
func TestPublishedPinOpenReplicaAndCatchUpFailClosed(t *testing.T) {
	src, err := OpenReplica(t.TempDir())
	if src != nil || err == nil || !strings.Contains(err.Error(), "control plane unavailable: published claudia v0.53.0") {
		t.Fatalf("OpenReplica = %v %v", src, err)
	}
	c := &Collector{}
	if err := c.CatchUpReplicas(context.Background()); err != nil {
		t.Fatalf("nil replica catch-up: %v", err)
	}
	c.replica = &ReplicaSource{}
	err = c.CatchUpReplicas(context.Background())
	if err == nil || !strings.Contains(err.Error(), "control plane unavailable: published claudia v0.53.0") {
		t.Fatalf("non-nil replica catch-up = %v", err)
	}
	_, err = c.ReplicatedReceipt(context.Background(), claudiawire.ReceiptQuery{
		Seat: "seat", SessionID: "generation", RequestID: "logical-A",
	})
	if err == nil || !strings.Contains(err.Error(), "replicated receipt: no replica") {
		t.Fatalf("replicated receipt = %v", err)
	}
}

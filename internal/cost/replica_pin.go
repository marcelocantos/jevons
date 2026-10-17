// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !jevons_claudia_next

package cost

import (
	"context"
	"fmt"

	"github.com/marcelocantos/jevons/internal/claudiawire"
)

// ReplicaSource is the published-pin stand-in. OpenReplica fails closed
// because claudia v0.53.0 does not ship the control plane. replica.go is
// the real source, and it builds only with jevons_claudia_next.
type ReplicaSource struct{}

// OpenReplica reports that the published pin has no control plane.
func OpenReplica(string) (*ReplicaSource, error) {
	return nil, fmt.Errorf("control plane unavailable: published claudia v0.53.0")
}

// CatchUpReplicas is a no-op when the daemon has no replica. A non-nil
// replica on this build is a programming error.
func (c *Collector) CatchUpReplicas(context.Context) error {
	if c == nil || c.replica == nil {
		return nil
	}
	return fmt.Errorf("control plane unavailable: published claudia v0.53.0")
}

// ReplicatedReceipt has no journal to read on the published pin.
func (c *Collector) ReplicatedReceipt(context.Context, claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
	return nil, fmt.Errorf("replicated receipt: no replica")
}

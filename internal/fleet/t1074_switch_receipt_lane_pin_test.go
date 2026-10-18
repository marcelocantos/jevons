// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !jevons_claudia_next

package fleet

import (
	"context"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudiawire"
)

// switchReceiptLane is inert on the published pin. The model-switch test
// never installed a receipt query, and this build does not invent one.
// Acceptance is the jevons_claudia_next lane.
type switchReceiptLane struct {
	live    bool
	publish func(seat, session, id string, state claudiawire.ReceiptState, cause claudiawire.ReceiptCause, reason string, turns []string)
	read    func(context.Context, string, string, string) (*claudiawire.RequestReceipt, error)
}

func openSwitchReceiptLane(t *testing.T, dir string) switchReceiptLane {
	t.Helper()
	_ = dir
	return switchReceiptLane{live: false}
}

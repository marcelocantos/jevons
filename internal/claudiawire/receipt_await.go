// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package claudiawire

import (
	"context"
	"errors"
	"time"
)

// ErrReceiptAbsent means the receipt store holds no row for the request.
// Jevons reads receipts from a replica of Claudia's control plane, and
// Claudia acknowledges an identified send before its seat pump publishes
// the receipt that send produced. A row missing straight after the send is
// therefore replication that has not arrived yet, not a refusal.
var ErrReceiptAbsent = errors.New("receipt absent")

// Poll interval bounds for AwaitReceipt. The first read is immediate.
const (
	receiptAwaitFirstPoll = 10 * time.Millisecond
	receiptAwaitMaxPoll   = 200 * time.Millisecond
)

// AwaitReceipt reads q through read and reads again while the row is
// absent, until it is present or ctx ends. It is for a handoff this
// process has just written, whose receipt is owed. Any other error, and
// any receipt that is present (whatever its state), returns at once:
// absence is the only answer that more time can change.
func AwaitReceipt(ctx context.Context, read func(context.Context, ReceiptQuery) (*RequestReceipt, error), q ReceiptQuery) (*RequestReceipt, error) {
	wait := receiptAwaitFirstPoll
	for {
		r, err := read(ctx, q)
		if err == nil || !errors.Is(err, ErrReceiptAbsent) {
			return r, err
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, err
		case <-timer.C:
		}
		if wait *= 2; wait > receiptAwaitMaxPoll {
			wait = receiptAwaitMaxPoll
		}
	}
}

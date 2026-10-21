// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// TestEscalatingDisconnectSettlesPriorReceipt drives escalateIfBusyWithRequestID,
// the busy-seat send. A prior uncertain row whose receipt is cancelled is
// removed before the new attempt. A hard disconnect removes the new row.
// A sentence that only says the call timed out removes it too. A
// context.DeadlineExceeded reply wait keeps only that new row.
func TestEscalatingDisconnectSettlesPriorReceipt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		remain bool
	}{
		{name: "disconnect", err: errors.New("sidecar disconnected before receipt")},
		{name: "quoted timeout", err: errors.New("operation timed out")},
		{name: "timeout", err: context.DeadlineExceeded, remain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBindFixture(t, "T1074")
			receiptJournal(t, f.s, t.TempDir())
			old := receiptHeldEntry(t, f.s.sendQueue(), f.name, "owner-question", true)
			f.probe.escalateErr = tc.err
			f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
				if q.RequestID == old.TransportRequestID {
					r := heldReceipt(q, claudiawire.ReceiptCancelled)
					r.Reason = "sidecar exited"
					return r, nil
				}
				return nil, errors.New("replicated receipt absent")
			})
			f.s.Seats().Observe(seatstate.Observation{
				Name: f.name, Alive: seatstate.Yes, InFlight: seatstate.Yes,
				QueueDepth: seatstate.QueueUnknown, Source: "test.provider",
			})
			_, handled, err := f.s.escalateIfBusyWithRequestID(f.name, "second marker", config.EscalationOwner, "jevons", f.probe, "owner-2")
			if err == nil || !handled {
				t.Fatalf("handled=%v err=%v", handled, err)
			}
			rows, rerr := f.s.sendQueue().Snapshot(f.name)
			if rerr != nil {
				t.Fatal(rerr)
			}
			for _, row := range rows {
				if row.ID == old.ID {
					t.Fatalf("cancelled receipt left uncertain row %+v", row)
				}
			}
			if tc.remain {
				if len(rows) != 1 || rows[0].State != sendq.Uncertain {
					t.Fatalf("timeout rows = %+v", rows)
				}
				return
			}
			if len(rows) != 0 {
				t.Fatalf("disconnect rows = %+v", rows)
			}
		})
	}
}

// TestSettleHeldReceiptsRemovesTerminalReceipt drives the shutdown pass.
// A cancelled receipt leaves the queue. A lookup error does not.
func TestSettleHeldReceiptsRemovesTerminalReceipt(t *testing.T) {
	f := newBindFixture(t, "T1074")
	receiptJournal(t, f.s, t.TempDir())
	settled := receiptHeldEntry(t, f.s.sendQueue(), f.name, "owner-question", true)
	held := receiptHeldEntry(t, f.s.sendQueue(), "other-seat", "owner-question", true)
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if q.Seat == f.name && q.RequestID == settled.TransportRequestID {
			r := heldReceipt(q, claudiawire.ReceiptCancelled)
			r.Reason = "sidecar exited"
			return r, nil
		}
		return nil, errors.New("replicated receipt absent")
	})
	f.s.SettleHeldReceipts()
	rows, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(rows) != 0 {
		t.Fatalf("settled seat rows = %+v err=%v", rows, err)
	}
	kept, err := f.s.sendQueue().Snapshot("other-seat")
	if err != nil || len(kept) != 1 || kept[0].ID != held.ID || kept[0].State != sendq.Uncertain {
		t.Fatalf("absent receipt rows = %+v err=%v", kept, err)
	}
}

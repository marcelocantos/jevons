// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

func transcriptMiss() turnWitness {
	return func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, Detail: "transcript has no payload"}
		}
	}
}

// TestT1074SettledReceiptIgnoresTranscriptAbsence is a capable immediate
// send. The witness looks and does not see the payload. The receipt
// completes, so the caller hears sent and the transcript is not recorded
// as delivery.
func TestT1074SettledReceiptIgnoresTranscriptAbsence(t *testing.T) {
	f := newBindFixture(t, "T1074")
	f.s.stateDir = t.TempDir()
	journal := receiptJournal(t, f.s, t.TempDir())
	f.s.SetTurnWitness(transcriptMiss())
	const payload = "owner: receipt settles without a transcript hit"
	var transportID string
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("sent %q", text)
		}
		transportID = id
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if transportID == "" || q.RequestID != transportID {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID)
		}
		return heldReceipt(q, claudiawire.ReceiptCompleted), nil
	})
	res, err := deliverObservedToSender(f.s, f.name, payload, false, f.probe, false)
	if err != nil || res.Status != "sent" {
		t.Fatalf("caller status=%q err=%v", res.Status, err)
	}
	if f.s.priorDelivery(f.name, payload) {
		t.Fatal("transcript absence still recorded delivery")
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("settled receipt left the queue: %+v %v", entries, err)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 1 || events[0].Fields["transport_request_id"] != transportID {
		t.Fatalf("journal: %+v %v", events, err)
	}
}

// TestT1074SettledReceiptIgnoresPayloadSeen is the same send while the
// witness reports PayloadSeen. That hit must not become delivery evidence.
func TestT1074SettledReceiptIgnoresPayloadSeen(t *testing.T) {
	f := newBindFixture(t, "T1074")
	f.s.stateDir = t.TempDir()
	receiptJournal(t, f.s, t.TempDir())
	const payload = "owner: payload seen is not the receipt"
	var transportID string
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("sent %q", text)
		}
		transportID = id
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if transportID == "" || q.RequestID != transportID {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID)
		}
		return heldReceipt(q, claudiawire.ReceiptCompleted), nil
	})
	res, err := deliverObservedToSender(f.s, f.name, payload, false, f.probe, false)
	if err != nil || res.Status != "sent" || res.ReceiverReceipt {
		t.Fatalf("caller status=%q receipt=%v err=%v", res.Status, res.ReceiverReceipt, err)
	}
	if f.s.priorDelivery(f.name, payload) {
		t.Fatal("PayloadSeen recorded delivery after the receipt settled")
	}
}

// TestT1074TimeoutSettlesFromReceipt is a capable send whose transport
// reports a reply timeout. The receipt completes. The empty transcript
// does not turn that into "not submitted".
func TestT1074TimeoutSettlesFromReceipt(t *testing.T) {
	f := newBindFixture(t, "T1074")
	f.s.stateDir = t.TempDir()
	receiptJournal(t, f.s, t.TempDir())
	f.s.SetTurnWitness(transcriptMiss())
	f.probe.fail = errors.New("operation timed out")
	const payload = "owner: timeout is not the receipt"
	var transportID string
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("sent %q", text)
		}
		transportID = id
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if transportID == "" || q.RequestID != transportID {
			t.Fatalf("queried %q, handoff %q", q.RequestID, transportID)
		}
		return heldReceipt(q, claudiawire.ReceiptCompleted), nil
	})
	res, err := deliverObservedToSender(f.s, f.name, payload, false, f.probe, false)
	if err != nil || res.Status != "sent" {
		t.Fatalf("caller status=%q err=%v", res.Status, err)
	}
	if f.s.priorDelivery(f.name, payload) {
		t.Fatal("timeout path recorded transcript delivery")
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("settled timeout left the queue: %+v %v", entries, err)
	}
}

// TestT1074InterruptBackstopSettlesFromReceipt cuts a turn, sees the first
// offer bounce as still busy, and sends again. PayloadSeen on that second
// offer used to answer interrupted_sent before the receipt was read. An
// unknown receipt stays delivered_unconfirmed. A completed receipt on a
// transcript miss answers interrupted_sent and does not record the miss.
func TestT1074InterruptBackstopSettlesFromReceipt(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		f := newBindFixture(t, "T1074")
		f.s.stateDir = t.TempDir()
		const payload = "owner: interrupt payload seen is not acceptance"
		f.probe.fail = claudia.ErrTurnInFlight
		var calls int
		var transportID string
		f.probe.check = func(text, id string) {
			if text != payload {
				t.Fatalf("sent %q", text)
			}
			calls++
			transportID = id
			if calls > 1 {
				f.probe.fail = nil
			}
		}
		f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
			if transportID == "" || q.RequestID != transportID {
				t.Fatalf("queried %q, handoff %q", q.RequestID, transportID)
			}
			r := heldReceipt(q, claudiawire.ReceiptUnknown)
			r.Reason = "not_found"
			r.Cause = ""
			return r, nil
		})
		res, err := deliverObservedToSender(f.s, f.name, payload, true, f.probe, false)
		if err != nil || res.Status != "delivered_unconfirmed" {
			t.Fatalf("caller status=%q err=%v calls=%d", res.Status, err, calls)
		}
		if calls != 2 {
			t.Fatalf("backstop sends=%d", calls)
		}
		if f.s.priorDelivery(f.name, payload) {
			t.Fatal("PayloadSeen recorded an unsettled interrupt")
		}
		entries, err := f.s.sendQueue().Snapshot(f.name)
		if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID != transportID {
			t.Fatalf("unknown interrupt receipt confirmed: %+v %v", entries, err)
		}
	})
	t.Run("settled", func(t *testing.T) {
		f := newBindFixture(t, "T1074")
		f.s.stateDir = t.TempDir()
		receiptJournal(t, f.s, t.TempDir())
		f.s.SetTurnWitness(transcriptMiss())
		const payload = "owner: interrupt receipt settles without a transcript hit"
		f.probe.fail = claudia.ErrTurnInFlight
		var calls int
		var transportID string
		f.probe.check = func(text, id string) {
			if text != payload {
				t.Fatalf("sent %q", text)
			}
			calls++
			transportID = id
			if calls > 1 {
				f.probe.fail = nil
			}
		}
		f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
			if transportID == "" || q.RequestID != transportID {
				t.Fatalf("queried %q, handoff %q", q.RequestID, transportID)
			}
			return heldReceipt(q, claudiawire.ReceiptCompleted), nil
		})
		res, err := deliverObservedToSender(f.s, f.name, payload, true, f.probe, false)
		if err != nil || res.Status != "interrupted_sent" {
			t.Fatalf("caller status=%q err=%v calls=%d", res.Status, err, calls)
		}
		if calls != 2 {
			t.Fatalf("backstop sends=%d", calls)
		}
		if f.s.priorDelivery(f.name, payload) {
			t.Fatal("interrupt transcript miss recorded delivery")
		}
		entries, err := f.s.sendQueue().Snapshot(f.name)
		if err != nil || len(entries) != 0 {
			t.Fatalf("settled interrupt left the queue: %+v %v", entries, err)
		}
	})
}

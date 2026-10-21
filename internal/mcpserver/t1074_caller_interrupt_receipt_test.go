// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// callerInterruptSend is the production interrupt backstop with the caller
// owning confirmation. The first offer bounces as still busy, the hatch
// cuts the turn, and the second offer is the one under test. Spawn passes
// interrupt false, so this is not the opening brief.
func callerInterruptSend(t *testing.T, f *bindFixture, payload string, second error) (agentSendResult, error, int, string) {
	t.Helper()
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
			f.probe.fail = second
		}
	}
	res, err := deliverObservedToSenderWith(f.s, f.name, payload, true, f.probe, false, confirmByCaller)
	return res, err, calls, transportID
}

// TestT1074CallerInterruptSettledReceiptIgnoresTranscriptAbsence is a
// capable interrupt whose caller owns confirmation. The second offer's
// receipt completes. The empty transcript does not refuse the send, and
// the mission frozen at bind stays that mission.
func TestT1074CallerInterruptSettledReceiptIgnoresTranscriptAbsence(t *testing.T) {
	f := newBindFixture(t, "T1074")
	f.s.stateDir = t.TempDir()
	receiptJournal(t, f.s, t.TempDir())
	f.s.SetTurnWitness(transcriptMiss())
	const payload = "owner: caller interrupt settles without a transcript hit"
	var queried string
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		queried = q.RequestID
		return heldReceipt(q, claudiawire.ReceiptCompleted), nil
	})
	res, err, calls, transportID := callerInterruptSend(t, f, payload, nil)
	if err != nil || res.Status != "interrupted_sent" || !res.ReceiptSettled {
		t.Fatalf("caller status=%q settled=%v err=%v calls=%d", res.Status, res.ReceiptSettled, err, calls)
	}
	if calls != 2 {
		t.Fatalf("backstop sends=%d", calls)
	}
	if queried != transportID || transportID == "" {
		t.Fatalf("queried %q, handoff %q", queried, transportID)
	}
	if f.s.priorDelivery(f.name, payload) {
		t.Fatal("transcript absence recorded delivery")
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("settled interrupt left the queue: %+v %v", entries, err)
	}
	got, ok, err := f.usage.RequestMission(context.Background(), f.name, transportID)
	if err != nil || !ok || got != "T1074" {
		t.Fatalf("mission after receipt = %q ok=%v err=%v", got, ok, err)
	}
}

// TestT1074CallerInterruptPayloadSeenDoesNotConfirmUnknownReceipt is the
// same interrupt while the witness reports PayloadSeen and the receipt is
// unknown. That hit used to answer interrupted_sent. The attempt stays
// uncertain and the frozen mission is not a transport acceptance.
func TestT1074CallerInterruptPayloadSeenDoesNotConfirmUnknownReceipt(t *testing.T) {
	f := newBindFixture(t, "T1074")
	f.s.stateDir = t.TempDir()
	const payload = "owner: caller interrupt payload seen is not acceptance"
	var queried string
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		queried = q.RequestID
		r := heldReceipt(q, claudiawire.ReceiptUnknown)
		r.Reason = "not_found"
		r.Cause = ""
		return r, nil
	})
	res, err, calls, transportID := callerInterruptSend(t, f, payload, nil)
	if err != nil || res.Status != "delivered_unconfirmed" || res.ReceiptSettled {
		t.Fatalf("caller status=%q settled=%v err=%v calls=%d", res.Status, res.ReceiptSettled, err, calls)
	}
	if calls != 2 {
		t.Fatalf("backstop sends=%d", calls)
	}
	if queried != transportID || transportID == "" {
		t.Fatalf("queried %q, handoff %q", queried, transportID)
	}
	if f.s.priorDelivery(f.name, payload) {
		t.Fatal("PayloadSeen recorded an unsettled caller interrupt")
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID != transportID {
		t.Fatalf("unknown interrupt receipt confirmed: %+v %v", entries, err)
	}
	got, ok, err := f.usage.RequestMission(context.Background(), f.name, transportID)
	if err != nil || !ok || got != "T1074" {
		t.Fatalf("mission after unknown receipt = %q ok=%v err=%v", got, ok, err)
	}
}

// TestT1074CallerInterruptTimeoutSettlesFromReceipt is the second offer
// timing out. That claim does not disprove delivery. The completed receipt
// answers interrupted_sent, and the empty transcript does not.
func TestT1074CallerInterruptTimeoutSettlesFromReceipt(t *testing.T) {
	f := newBindFixture(t, "T1074")
	f.s.stateDir = t.TempDir()
	receiptJournal(t, f.s, t.TempDir())
	f.s.SetTurnWitness(transcriptMiss())
	const payload = "owner: caller interrupt timeout is not the receipt"
	var queried string
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		queried = q.RequestID
		return heldReceipt(q, claudiawire.ReceiptCompleted), nil
	})
	res, err, calls, transportID := callerInterruptSend(t, f, payload, context.DeadlineExceeded)
	if err != nil || res.Status != "interrupted_sent" || !res.ReceiptSettled {
		t.Fatalf("caller status=%q settled=%v err=%v calls=%d", res.Status, res.ReceiptSettled, err, calls)
	}
	if calls != 2 {
		t.Fatalf("backstop sends=%d", calls)
	}
	if queried != transportID || transportID == "" {
		t.Fatalf("queried %q, handoff %q", queried, transportID)
	}
	if f.s.priorDelivery(f.name, payload) {
		t.Fatal("timeout path recorded transcript delivery")
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("settled timeout left the queue: %+v %v", entries, err)
	}
}

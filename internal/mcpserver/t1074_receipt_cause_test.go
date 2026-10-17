// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// TestT1074BoundDrainQueryErrorStaysHeld is the witness decoy. t418Daemon
// reports PayloadSeen for the drained text. A query error is not delivery.
func TestT1074BoundDrainQueryErrorStaysHeld(t *testing.T) {
	f := newBindFixture(t, "T1074")
	const payload = "do not confirm from the transcript"
	if _, _, err := f.s.sendQueue().Append(f.name, payload, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, _ claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		return nil, errors.New("journal corrupt or transport unavailable")
	})
	f.s.drainAgentSendQueue(f.name)
	if len(f.probe.sentIDs()) != 1 {
		t.Fatalf("sends=%v", f.probe.sentIDs())
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID == "" {
		t.Fatalf("query error confirmed from PayloadSeen: %+v %v", entries, err)
	}
}

// TestT1074ActiveSteerLaterRequestSettlesOnlyItsOwnReceipt drives one active
// turn, a steer, and a later request that repeats the first text. The witness
// reports PayloadSeen for all three. Bound drains settle only from the receipt
// queried for that transport id. Steer still confirms from a nil send error
// and does not read a receipt; this slice did not change that defer.
func TestT1074ActiveSteerLaterRequestSettlesOnlyItsOwnReceipt(t *testing.T) {
	f := newBindFixture(t, "T1074")
	journal := receiptJournal(t, f.s, t.TempDir())
	const text = "same text"
	var ids []string
	f.probe.check = func(sent, id string) {
		if sent != text {
			t.Fatalf("sent %q", sent)
		}
		f.assertBoundBeforeSend(t, text, id, "T1074")
		ids = append(ids, id)
	}
	phase := "A"
	var queries []string
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		queries = append(queries, q.RequestID)
		if phase == "steer" {
			t.Fatalf("steer read a receipt: %+v", q)
		}
		if len(ids) == 0 || q.RequestID != ids[len(ids)-1] {
			t.Fatalf("phase %s queried %q while the handoff ids are %v", phase, q.RequestID, ids)
		}
		r := heldReceipt(q, claudia.ReceiptCompleted)
		switch phase {
		case "A":
			// Reason names an auth failure. The typed cause is empty, so the
			// consumer must not classify this as auth.
			r.Reason = "401 unauthorized"
			r.Cause = ""
			r.TurnIDs = []string{"turn-A"}
		case "C":
			if q.RequestID == ids[0] {
				t.Fatal("later request inherited the active turn's receipt")
			}
			r.State = claudia.ReceiptFailed
			r.Cause = claudia.ReceiptCauseAuth
			r.Reason = "provider stopped"
			r.TurnIDs = []string{"turn-C"}
		default:
			t.Fatalf("receipt query during %s", phase)
		}
		return r, nil
	})

	if _, _, err := f.s.sendQueue().Append(f.name, text, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.s.drainAgentSendQueue(f.name)
	if len(ids) != 1 {
		t.Fatalf("active turn was not sent: %v", ids)
	}
	if entries, err := f.s.sendQueue().Snapshot(f.name); err != nil || len(entries) != 0 {
		t.Fatalf("active turn still queued: %+v %v", entries, err)
	}

	phase = "steer"
	queriesBeforeSteer := len(queries)
	res, err := deliverObservedToSenderMode(f.s, f.name, text, delivery.ModeSteer, f.probe, false, confirmHere)
	if err != nil || res.Status != "steered" {
		t.Fatalf("steer: status=%q err=%v", res.Status, err)
	}
	if len(queries) != queriesBeforeSteer {
		t.Fatalf("steer queried receipts: %v", queries[queriesBeforeSteer:])
	}
	if len(ids) != 2 {
		t.Fatalf("steer ids=%v", ids)
	}
	if entries, err := f.s.sendQueue().Snapshot(f.name); err != nil || len(entries) != 0 {
		t.Fatalf("steer left the queue: %+v %v", entries, err)
	}

	phase = "C"
	if _, _, err := f.s.sendQueue().Append(f.name, text, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.s.drainAgentSendQueue(f.name)
	if len(ids) != 3 || ids[0] == ids[1] || ids[0] == ids[2] || ids[1] == ids[2] {
		t.Fatalf("transport ids are not distinct: %v", ids)
	}
	if entries, err := f.s.sendQueue().Snapshot(f.name); err != nil || len(entries) != 0 {
		t.Fatalf("later request still queued: %+v %v", entries, err)
	}
	if len(f.probe.sentIDs()) != 3 {
		t.Fatalf("sends=%v", f.probe.sentIDs())
	}

	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 2 {
		t.Fatalf("journal: %+v %v", events, err)
	}
	byID := map[string]map[string]any{}
	for _, ev := range events {
		id, _ := ev.Fields["transport_request_id"].(string)
		byID[id] = ev.Fields
	}
	a := byID[ids[0]]
	c := byID[ids[2]]
	if a == nil || c == nil {
		t.Fatalf("journal ids=%v fields=%v", ids, byID)
	}
	if a["receipt_cause"] != "" {
		t.Fatalf("prose reason became a cause: %+v", a)
	}
	ar, _ := a["receipt"].(map[string]any)
	if ar["reason"] != "401 unauthorized" || ar["cause"] != nil {
		t.Fatalf("active receipt: %+v", ar)
	}
	if c["receipt_cause"] != string(claudia.ReceiptCauseAuth) {
		t.Fatalf("typed cause was not journaled: %+v", c)
	}
	cr, _ := c["receipt"].(map[string]any)
	if cr["cause"] != string(claudia.ReceiptCauseAuth) || cr["reason"] != "provider stopped" || cr["state"] != string(claudia.ReceiptFailed) {
		t.Fatalf("later receipt: %+v", cr)
	}
	if _, ok := byID[ids[1]]; ok {
		t.Fatal("steer wrote a receipt journal row")
	}
	mission, ok, err := f.usage.RequestMission(context.Background(), f.name, ids[0])
	if err != nil || !ok || mission != "T1074" {
		t.Fatalf("mission=%q ok=%v err=%v", mission, ok, err)
	}
}

// TestT1074FailedSubmissionDoesNotInventCause is a send that returns before
// any durable acceptance. The error text says unauthorized. The receipt is
// unknown, so the attempt stays held and no cause is journaled. After the
// seat model changes, the same text under a new transport id settles only
// from its own receipt, whose prose reason is not a cause.
func TestT1074FailedSubmissionDoesNotInventCause(t *testing.T) {
	f := newBindFixture(t, "T1074")
	journal := receiptJournal(t, f.s, t.TempDir())
	const text = "same text"
	f.probe.fail = errors.New("401 unauthorized")
	var first string
	f.probe.check = func(sent, id string) {
		if sent != text {
			t.Fatalf("sent %q", sent)
		}
		if first == "" {
			first = id
		}
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
		if first == "" || q.RequestID == first {
			r := heldReceipt(q, claudia.ReceiptUnknown)
			r.Reason = "not_found"
			r.Cause = ""
			return r, nil
		}
		r := heldReceipt(q, claudia.ReceiptCompleted)
		r.Reason = "401 unauthorized"
		r.Cause = ""
		r.TurnIDs = []string{"turn-B"}
		return r, nil
	})
	if _, _, err := f.s.sendQueue().Append(f.name, text, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.s.drainAgentSendQueue(f.name)
	if len(f.probe.sentIDs()) != 1 || first == "" {
		t.Fatalf("failed submission was not attempted: %v", f.probe.sentIDs())
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID != first {
		t.Fatalf("failed submission was confirmed: %+v %v", entries, err)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 0 {
		t.Fatalf("unknown receipt was journaled as a cause: %+v %v", events, err)
	}

	d := *f.s.registry.Def(f.name)
	d.Model = "grok-4"
	if err := f.s.registry.Register(d); err != nil {
		t.Fatal(err)
	}
	f.probe.fail = nil
	if _, _, err := f.s.sendQueue().Append(f.name, text, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.s.drainAgentSendQueue(f.name)
	if len(f.probe.sentIDs()) != 2 || f.probe.sentIDs()[1] == first {
		t.Fatalf("later request reused the failed submission: %v", f.probe.sentIDs())
	}
	entries, err = f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].TransportRequestID != first || entries[0].State != sendq.Uncertain {
		t.Fatalf("failed submission was released by the later receipt: %+v %v", entries, err)
	}
	def := f.s.registry.Def(f.name)
	if def.Model != "grok-4" || def.SessionID != f.session || def.TargetID != "T1074" {
		t.Fatalf("model change reminted the seat: %+v", def)
	}
	events, err = eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 1 {
		t.Fatalf("journal: %+v %v", events, err)
	}
	if events[0].Fields["transport_request_id"] == first || events[0].Fields["receipt_cause"] != "" {
		t.Fatalf("later receipt inherited a cause: %+v", events[0].Fields)
	}
	receipt, _ := events[0].Fields["receipt"].(map[string]any)
	if receipt["reason"] != "401 unauthorized" || receipt["cause"] != nil || receipt["state"] != string(claudia.ReceiptCompleted) {
		t.Fatalf("later receipt: %+v", receipt)
	}
}

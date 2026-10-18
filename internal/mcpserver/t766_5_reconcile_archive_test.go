// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// The operator tool is the shipped confirmed, drop, and consolidate path.
// An empty queue and an eventlog line are not the payload record.
func TestT7665ToolConfirmedAndDropArchiveThePayload(t *testing.T) {
	const name = "claudia-po"
	s, _, _, held := pinnedDaemon(t, name, "confirmed tool copy")
	res := reconcile(t, s, map[string]any{
		"name": name, "action": "confirmed", "entry_id": held.ID, "attempt_id": held.AttemptID,
		"actor": "jevons-po", "evidence": "receiver JSONL has the user message",
	})
	if res.IsError {
		t.Fatalf("confirmed refused: %s", resultText(t, res))
	}
	assertArchived(t, s, held.ID, "confirmed tool copy")
	if depth := observedPendingSends(s, name); depth != 0 {
		t.Fatalf("depth = %d", depth)
	}

	s, _, _, held = pinnedDaemon(t, name, "dropped tool copy")
	res = reconcile(t, s, map[string]any{
		"name": name, "action": "drop", "entry_id": held.ID, "attempt_id": held.AttemptID,
		"actor": "jevons-po", "evidence": "owner abandoned this copy",
	})
	if res.IsError {
		t.Fatalf("drop refused: %s", resultText(t, res))
	}
	assertArchived(t, s, held.ID, "dropped tool copy")
}

func TestT7665ToolRequeueDoesNotArchive(t *testing.T) {
	const name = "claudia-po"
	s, _, _, held := pinnedDaemon(t, name, "requeued tool copy")
	res := reconcile(t, s, map[string]any{
		"name": name, "action": "requeue", "entry_id": held.ID, "attempt_id": held.AttemptID,
		"actor": "jevons-po", "evidence": "transcript has no copy",
	})
	if res.IsError {
		t.Fatalf("requeue refused: %s", resultText(t, res))
	}
	left, err := s.sendQueue().Snapshot(name)
	if err != nil || len(left) != 1 || left[0].ID != held.ID || left[0].Text != "requeued tool copy" || left[0].State != sendq.Pending {
		t.Fatalf("requeue = %+v %v", left, err)
	}
	if _, err := s.sendQueue().ReadSupersession(held.ID); err == nil {
		t.Fatal("requeue archived the payload")
	}
}

func TestT7665ToolConsolidateArchivesTheFoldedPayloads(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return nil, false, nil })
	const name = "ge-t191-ndk-discovery"
	now := time.Now()
	first, _, err := s.sendQueue().Append(name, "do A", now)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.sendQueue().Append(name, "actually do B", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	res := reconcile(t, s, map[string]any{
		"name": name, "action": "consolidate", "keep": second.ID, "text": "authoritative: do B",
		"actor": "jevons-po", "evidence": "do A is superseded by actually do B",
	})
	if res.IsError {
		t.Fatalf("consolidate refused: %s", resultText(t, res))
	}
	left, err := s.sendQueue().Snapshot(name)
	if err != nil || len(left) != 1 || left[0].ID != second.ID || left[0].Text != "authoritative: do B" {
		t.Fatalf("survivor = %+v %v", left, err)
	}
	assertArchived(t, s, first.ID, "do A")
	if _, err := s.sendQueue().ReadSupersession(second.ID); err == nil {
		t.Fatal("survivor was archived")
	}
}

// A bound drain that stays uncertain, then sees an accepted receipt, settles
// through Reconcile confirmed. That is not the operator tool. The payload
// still has to be archived under the entry id.
func TestT7665ReceiptRecoveryArchivesTheConfirmedPayload(t *testing.T) {
	f := newBindFixture(t, "T1074")
	const payload = "receipt recovery copy that must survive"
	admitted, _, err := f.s.sendQueue().Append(f.name, payload, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var transportID string
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("sent %q", text)
		}
		transportID = id
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		return heldReceipt(q, claudiawire.ReceiptUnknown), nil
	})
	f.s.drainAgentSendQueue(f.name)
	left, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(left) != 1 || left[0].ID != admitted.ID || left[0].State != sendq.Uncertain || left[0].TransportRequestID != transportID || transportID == "" {
		t.Fatalf("first drain = %+v id=%q %v", left, transportID, err)
	}
	if _, err := f.s.sendQueue().ReadSupersession(admitted.ID); err == nil {
		t.Fatal("unsettled receipt archived the payload")
	}
	receiptJournal(t, f.s, t.TempDir())
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		if q.RequestID != transportID {
			t.Fatalf("recovery queried %q, want %q", q.RequestID, transportID)
		}
		return heldReceipt(q, claudiawire.ReceiptAccepted), nil
	})
	f.s.drainAgentSendQueue(f.name)
	if len(f.probe.sentIDs()) != 1 {
		t.Fatalf("recovery sent again: %v", f.probe.sentIDs())
	}
	left, err = f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(left) != 0 {
		t.Fatalf("settled queue = %+v %v", left, err)
	}
	assertArchived(t, f.s, admitted.ID, payload)
}

func assertArchived(t *testing.T, s *Server, id, text string) {
	t.Helper()
	got, err := s.sendQueue().ReadSupersession(id)
	if err != nil || len(got.Removed) != 1 || got.Removed[0].ID != id || got.Removed[0].Text != text {
		t.Fatalf("archive %s = %+v %v", id, got, err)
	}
}

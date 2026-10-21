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

// A bound drain that sees an accepted receipt resolves Confirmed and the
// row leaves the queue. That drop is Resolve, and the payload has to be
// archived under the entry id. An empty queue is not the record.
func TestT7665ConfirmedDrainArchivesThePayload(t *testing.T) {
	f := newBindFixture(t, "T1074")
	receiptJournal(t, f.s, t.TempDir())
	const payload = "confirmed drain copy that must survive"
	admitted, _, err := f.s.sendQueue().Append(f.name, payload, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		return heldReceipt(q, claudiawire.ReceiptAccepted), nil
	})
	f.s.drainAgentSendQueue(f.name)
	if len(f.probe.sentIDs()) != 1 {
		t.Fatalf("sends=%v", f.probe.sentIDs())
	}
	left, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(left) != 0 {
		t.Fatalf("confirmed drain left %+v %v", left, err)
	}
	archived, err := f.s.sendQueue().ReadSupersession(admitted.ID)
	if err != nil || len(archived.Removed) != 1 || archived.Removed[0].ID != admitted.ID || archived.Removed[0].Text != payload {
		t.Fatalf("archive = %+v %v", archived, err)
	}
}

// A live seat that refuses with a turn-in-flight cause resolves
// DefinitelyNotSent. The same entry stays pending and is not archived.
func TestT7665NotSentDrainDoesNotArchive(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	const name = "jv-t7665-not-sent"
	const payload = "not sent copy that stays queued"
	proc := &retrySender{fail: "cursor acp: prompt already in flight"}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return proc, false, nil })
	admitted, _, err := s.sendQueue().Append(name, payload, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue(name)
	left, err := s.sendQueue().Snapshot(name)
	if err != nil || len(left) != 1 || left[0].ID != admitted.ID || left[0].State != sendq.Pending || left[0].Text != payload || left[0].AttemptID != "" {
		t.Fatalf("not-sent drain = %+v %v", left, err)
	}
	if _, err := s.sendQueue().ReadSupersession(admitted.ID); err == nil {
		t.Fatal("not-sent drain archived the payload")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// t1081ReplicationLag is how long after the send returns the receipt row
// reaches the replica in these tests. J38 lost a 1 ms race; this is wide
// enough that a single read straight after the send always misses it.
const t1081ReplicationLag = 50 * time.Millisecond

// laggingReplica is the replica read as Claudia's ordering makes it: the
// identified send returns when the sidecar admits the request, and the
// receipt row arrives afterwards. Until then the row is absent.
type laggingReplica struct {
	mu        sync.Mutex
	published map[string]claudiawire.ReceiptState
	reads     int
}

func (l *laggingReplica) publishAfterAck(id string, state claudiawire.ReceiptState) {
	go func() {
		time.Sleep(t1081ReplicationLag)
		l.mu.Lock()
		l.published[id] = state
		l.mu.Unlock()
	}()
}

func (l *laggingReplica) query(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reads++
	state, ok := l.published[q.RequestID]
	if !ok {
		return nil, fmt.Errorf("replicated %w", claudiawire.ErrReceiptAbsent)
	}
	return heldReceipt(q, state), nil
}

func (l *laggingReplica) readCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reads
}

// 🎯T1081 (J38): the overseer's question to a busy worker is steered into
// the running turn through escalateIfBusyWithRequestID, the production
// caller. Claudia admits it, and its receipt reaches the replica after the
// send returns. The steer is reported steered and its row settles.
func TestT1081SteerIntoBusySeatSettlesReceiptThatArrivesAfterTheAck(t *testing.T) {
	f := newBindFixture(t, "T1081")
	journal := receiptJournal(t, f.s, t.TempDir())
	lag := &laggingReplica{published: map[string]claudiawire.ReceiptState{}}
	f.s.SetSendReceiptQuery(lag.query)
	observeSenderFixture(f.s, f.name, f.probe)
	f.s.noteTurnInFlight(f.name)
	const payload = "Overseer here: what is the capital of France? Answer in one word, then carry on."
	var transportID string
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("steered %q", text)
		}
		transportID = id
		lag.publishAfterAck(id, claudiawire.ReceiptAccepted)
	}
	res, handled, err := f.s.escalateIfBusyWithRequestID(f.name, payload, config.EscalationOverseer, "jevons", f.probe, "")
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if res.Status != "steered" {
		t.Fatalf("status=%q message=%q; an admitted steer whose receipt arrives after the ack was left unconfirmed", res.Status, res.Message)
	}
	if lag.readCount() < 2 {
		t.Fatalf("receipt reads = %d; the absent row was not read again", lag.readCount())
	}
	if entries, err := f.s.sendQueue().Snapshot(f.name); err != nil || len(entries) != 0 {
		t.Fatalf("settled steer left the queue: %+v %v", entries, err)
	}
	if _, held := f.s.pendingUnconfirmedSend(f.name, time.Now()); held {
		t.Fatal("settled steer is still remembered as undecided")
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 1 || events[0].Fields["transport_request_id"] != transportID {
		t.Fatalf("journal: %+v %v (transport %q)", events, err, transportID)
	}
}

// 🎯T1081: a message held behind a busy turn drains at the turn boundary
// through drainAgentSendQueue. Its receipt also arrives after the ack, and
// the drain confirms it instead of leaving it uncertain.
func TestT1081DrainSettlesReceiptThatArrivesAfterTheAck(t *testing.T) {
	f := newBindFixture(t, "T1081")
	journal := receiptJournal(t, f.s, t.TempDir())
	lag := &laggingReplica{published: map[string]claudiawire.ReceiptState{}}
	f.s.SetSendReceiptQuery(lag.query)
	const payload = "held behind the turn; deliver at its end"
	if _, _, err := f.s.sendQueue().Append(f.name, payload, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.probe.check = func(text, id string) {
		if text != payload {
			t.Fatalf("drained %q", text)
		}
		lag.publishAfterAck(id, claudiawire.ReceiptStarted)
	}
	f.s.drainAgentSendQueue(f.name)
	if ids := f.probe.sentIDs(); len(ids) != 1 {
		t.Fatalf("drain sent %v", ids)
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		var states []sendq.DeliveryState
		for _, e := range entries {
			states = append(states, e.State)
		}
		t.Fatalf("drained message whose receipt arrived after the ack stayed held: states=%v %v", states, err)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 1 {
		t.Fatalf("journal: %+v %v", events, err)
	}
}

// A row that never arrives is still unconfirmed, and the wait ends at the
// caller's deadline rather than spinning: the await widens a race, it does
// not invent a delivery.
func TestT1081AbsentReceiptStillUnconfirmedAtDeadline(t *testing.T) {
	f := newBindFixture(t, "T1081")
	receiptJournal(t, f.s, t.TempDir())
	lag := &laggingReplica{published: map[string]claudiawire.ReceiptState{}}
	f.s.SetSendReceiptQuery(lag.query)
	const payload = "admitted, receipt never replicated"
	if _, _, err := f.s.sendQueue().Append(f.name, payload, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	f.s.drainAgentSendQueueWithReceiptContext(ctx, f.name)
	if waited := time.Since(start); waited > 5*time.Second {
		t.Fatalf("drain waited %s past a 300ms deadline", waited)
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain {
		t.Fatalf("absent receipt was settled: %+v %v", entries, err)
	}
}

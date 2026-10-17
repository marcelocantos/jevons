// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/sendq"
)

func countingWitness(calls *int) turnWitness {
	return func(string, string) turnWatch {
		*calls++
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, PayloadSeen: true}
		}
	}
}

// drainAgentSendQueue is the production drain. A bound handoff settles from
// its receipt. Opening the transcript watch is not part of that answer.
func TestT1074BoundDrainDoesNotOpenTranscriptWatch(t *testing.T) {
	f := newBindFixture(t, "T1074")
	receiptJournal(t, f.s, t.TempDir())
	var calls int
	f.s.SetTurnWitness(countingWitness(&calls))
	const payload = "bound drain must not watch the transcript"
	if _, _, err := f.s.sendQueue().Append(f.name, payload, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.s.SetSendReceiptQuery(func(_ context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
		return heldReceipt(q, claudiawire.ReceiptAccepted), nil
	})
	f.s.drainAgentSendQueue(f.name)
	if calls != 0 {
		t.Fatalf("bound drain opened the transcript watch %d times", calls)
	}
	entries, err := f.s.sendQueue().Snapshot(f.name)
	if err != nil || len(entries) != 0 {
		t.Fatalf("accepted receipt left the queue: %+v %v", entries, err)
	}
	got, ok, err := f.usage.RequestMission(context.Background(), f.name, f.probe.sentIDs()[0])
	if err != nil || !ok || got != "T1074" {
		t.Fatalf("mission = %q ok=%v err=%v", got, ok, err)
	}
}

// An incapable drain has no receipt to settle. The transcript watch still
// must not run, and PayloadSeen must not clear the queue.
func TestT1074UnboundDrainDoesNotOpenTranscriptWatch(t *testing.T) {
	s, sender, _ := t418Daemon(t, t.TempDir())
	var calls int
	s.SetTurnWitness(countingWitness(&calls))
	const name = "jv-t1074-unbound-watch"
	const payload = "owner: no receipt and no watch"
	if _, _, err := s.sendQueue().Append(name, payload, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue(name)
	if calls != 0 {
		t.Fatalf("unbound drain opened the transcript watch %d times", calls)
	}
	if got := sender.delivered(); len(got) != 1 || got[0] != payload {
		t.Fatalf("delivered = %v", got)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID != "" {
		t.Fatalf("unbound drain = %+v %v", entries, err)
	}
	if !strings.Contains(entries[0].Detail, "no receipt") {
		t.Fatalf("detail = %q", entries[0].Detail)
	}
	if s.agentHasTurnBegan(name) {
		t.Fatal("transcript started the turn")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// An incapable sender has no host request id. PayloadSeen used to resolve
// the drain Confirmed and start the turn. The handoff now stays held.
func TestT1074UnboundDrainPayloadSeenDoesNotConfirm(t *testing.T) {
	s, sender, _ := t418Daemon(t, t.TempDir())
	const name = "jv-t1074-unbound"
	const payload = "owner: land this without a receipt"
	if _, _, err := s.sendQueue().Append(name, payload, time.Now()); err != nil {
		t.Fatal(err)
	}

	s.drainAgentSendQueue(name)
	if got := sender.delivered(); len(got) != 1 || got[0] != payload {
		t.Fatalf("delivered = %v", got)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain || entries[0].TransportRequestID != "" {
		t.Fatalf("transcript confirmed an unbound drain: %+v %v", entries, err)
	}
	if !strings.Contains(entries[0].Detail, "no receipt") {
		t.Fatalf("detail = %q", entries[0].Detail)
	}
	if s.agentHasTurnBegan(name) {
		t.Fatal("transcript started the turn")
	}

	s.drainAgentSendQueue(name)
	if got := sender.delivered(); len(got) != 1 {
		t.Fatalf("unbound uncertain entry was offered again: %v", got)
	}
}

// A queued-behind-turn reading used to confirm even when the transport
// proved it never sent. The refusal still returns the entry to Pending.
func TestT1074UnboundBusyRefusalStillRetries(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	const name = "jv-t1074-unbound-retry"
	proc := &retrySender{fail: "cursor acp: prompt already in flight"}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return proc, false, nil })
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadQueued: true}))
	if _, _, err := s.sendQueue().Append(name, "retry me", time.Now()); err != nil {
		t.Fatal(err)
	}

	s.drainAgentSendQueue(name)
	if got := proc.delivered(); len(got) != 1 {
		t.Fatalf("first refusal delivered %v", got)
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Pending || entries[0].AttemptID != "" {
		t.Fatalf("busy refusal was not returned to pending: %+v %v", entries, err)
	}
	if s.agentHasTurnBegan(name) {
		t.Fatal("a refusal started the turn")
	}

	proc.fail = ""
	s.drainAgentSendQueue(name)
	if got := proc.delivered(); len(got) != 2 {
		t.Fatalf("retry delivered %v", got)
	}
	entries, err = s.sendQueue().Snapshot(name)
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Uncertain {
		t.Fatalf("retried unbound send was confirmed from the transcript: %+v %v", entries, err)
	}
	if s.agentHasTurnBegan(name) {
		t.Fatal("the retry started the turn from the transcript")
	}
}

type retrySender struct {
	recordingSender
	fail string
}

func (r *retrySender) Send(text string) error {
	if err := r.recordingSender.Send(text); err != nil {
		return err
	}
	if r.fail != "" {
		return fmt.Errorf("%s", r.fail)
	}
	return nil
}

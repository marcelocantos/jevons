// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// dequeueAgentSend is the shipped pop. A claimable admission leaves the
// queue only after its text is archived. An in-flight attempt and a requeue
// stay, and neither is archived.
func TestT7665DequeueArchivesTheClaimablePayload(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	const name = "jevons-po"
	const payload = "claimable dequeue copy"
	if _, err := s.enqueueAgentSend(name, payload); err != nil {
		t.Fatal(err)
	}
	queued, err := s.sendQueue().Snapshot(name)
	if err != nil || len(queued) != 1 || queued[0].Text != payload {
		t.Fatalf("queued = %+v %v", queued, err)
	}
	got := s.dequeueAgentSend(name)
	if got.ID != queued[0].ID || got.Text != payload {
		t.Fatalf("dequeue = %+v", got)
	}
	left, err := s.sendQueue().Snapshot(name)
	if err != nil || len(left) != 0 {
		t.Fatalf("queue after dequeue = %+v %v", left, err)
	}
	assertArchived(t, s, queued[0].ID, payload)
	if again := s.dequeueAgentSend(name); again.Text != "" || again.ID != "" {
		t.Fatalf("second dequeue = %+v", again)
	}
}

func TestT7665DequeueLeavesInFlightAndRequeueUnarchived(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	const name = "jevons-po"
	if _, err := s.enqueueAgentSend(name, "in flight copy"); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok || attempt.Text != "in flight copy" {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	if got := s.dequeueAgentSend(name); got.ID != "" || got.Text != "" {
		t.Fatalf("in-flight dequeue = %+v", got)
	}
	left, err := s.sendQueue().Snapshot(name)
	if err != nil || len(left) != 1 || left[0].ID != attempt.ID || left[0].Text != "in flight copy" || left[0].State != sendq.Attempting {
		t.Fatalf("in-flight queue = %+v %v", left, err)
	}
	if _, err := s.sendQueue().ReadSupersession(attempt.ID); err == nil {
		t.Fatal("in-flight dequeue archived the payload")
	}

	const other = "claudia-po"
	if _, err := s.enqueueAgentSend(other, "requeued tool copy"); err != nil {
		t.Fatal(err)
	}
	held, ok, err := s.sendQueue().ClaimFront(other)
	if err != nil || !ok {
		t.Fatalf("claim requeue: %+v %v %v", held, ok, err)
	}
	if err := s.sendQueue().Resolve(other, held, sendq.Unverified, "no receiver evidence"); err != nil {
		t.Fatal(err)
	}
	// The fixture sender is alive. Requeue would otherwise kick an async drain
	// that rewrites the queue during TempDir cleanup. This oracle is the
	// requeue mutation, which must not archive.
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return nil, false, nil })
	res := reconcile(t, s, map[string]any{
		"name": other, "action": "requeue", "entry_id": held.ID, "attempt_id": held.AttemptID,
		"actor": "jevons-po", "evidence": "transcript has no copy",
	})
	if res.IsError {
		t.Fatalf("requeue refused: %s", resultText(t, res))
	}
	requeued, err := s.sendQueue().Snapshot(other)
	if err != nil || len(requeued) != 1 || requeued[0].ID != held.ID || requeued[0].Text != "requeued tool copy" || requeued[0].State != sendq.Pending {
		t.Fatalf("requeue = %+v %v", requeued, err)
	}
	if _, err := s.sendQueue().ReadSupersession(held.ID); err == nil {
		t.Fatal("requeue archived the payload")
	}
}

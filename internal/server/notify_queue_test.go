// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"strings"
	"testing"
)

func TestNotifyCoalesceKey(t *testing.T) {
	idleA := "[event: worker-idle] A work agent under you entered phase=idle\n\nWorker: jv-a\nTarget: 🎯T1\n"
	idleA2 := "[event: worker-idle] A work agent under you entered phase=idle\n\nWorker: jv-a\nTarget: 🎯T1\n(later)"
	idleB := "[event: worker-idle] …\nWorker: jv-b\n"
	agentA := "[Agent jv-a responded]\nfirst body"
	agentA2 := "[Agent jv-a responded]\nsecond body"
	owner := userTurnPrefix + "hello"
	other := "budget alert: burn high"

	if k := notifyCoalesceKey(idleA); k != "idle:jv-a" {
		t.Fatalf("idleA key=%q", k)
	}
	if notifyCoalesceKey(idleA) != notifyCoalesceKey(idleA2) {
		t.Fatal("same worker idle must share key")
	}
	if notifyCoalesceKey(idleA) == notifyCoalesceKey(idleB) {
		t.Fatal("different workers must not share idle key")
	}
	if k := notifyCoalesceKey(agentA); k != "agent:jv-a" {
		t.Fatalf("agentA key=%q", k)
	}
	if notifyCoalesceKey(agentA) != notifyCoalesceKey(agentA2) {
		t.Fatal("same agent response key")
	}
	if notifyCoalesceKey(owner) != "" {
		t.Fatal("owner turns must not coalesce")
	}
	if notifyCoalesceKey(other) != "" {
		t.Fatal("unkeyed notes must not coalesce")
	}
}

func TestCoalesceNotifyEnqueueSameWorkerIdle(t *testing.T) {
	var q []string
	for i := 0; i < 20; i++ {
		note := fmt.Sprintf("[event: worker-idle] tick %d\nWorker: jevons-po\n", i)
		q = coalesceNotifyEnqueue(q, note)
	}
	if len(q) != 1 {
		t.Fatalf("same-worker idle flood want depth 1, got %d: %v", len(q), q)
	}
	if !strings.Contains(q[0], "tick 19") {
		t.Fatalf("want latest idle body, got %q", q[0])
	}
}

func TestCoalesceNotifyEnqueueOnePerWorker(t *testing.T) {
	var q []string
	workers := []string{"jv-a", "jv-b", "jv-c", "jevons-po"}
	for round := 0; round < 5; round++ {
		for _, w := range workers {
			q = coalesceNotifyEnqueue(q, fmt.Sprintf(
				"[event: worker-idle] r%d\nWorker: %s\n", round, w))
		}
	}
	if len(q) != len(workers) {
		t.Fatalf("want %d pending (one per worker), got %d", len(workers), len(q))
	}
	// Latest round wins per worker.
	for _, n := range q {
		if !strings.Contains(n, "r4") {
			t.Fatalf("want latest round body, got %q", n)
		}
	}
}

func TestTakeNotifyDrainBatchOwnerFirst(t *testing.T) {
	q := []string{
		"[event: worker-idle]\nWorker: jv-a\n",
		"[Agent jv-b responded]\nok",
		userTurnPrefix + "owner says hi",
		"[event: worker-idle]\nWorker: jv-c\n",
	}
	batch, rest, owner := takeNotifyDrainBatch(q)
	if !owner || len(batch) != 1 || !isOwnerNotifyText(batch[0]) {
		t.Fatalf("want single owner batch, got owner=%v batch=%v", owner, batch)
	}
	if len(rest) != 3 {
		t.Fatalf("rest depth=%d want 3", len(rest))
	}
	// Remaining drain is all fleet, one batch.
	batch2, rest2, owner2 := takeNotifyDrainBatch(rest)
	if owner2 || len(batch2) != 3 || len(rest2) != 0 {
		t.Fatalf("fleet batch owner=%v batch=%d rest=%d", owner2, len(batch2), len(rest2))
	}
}

func TestRequeueNotifyFrontRecoalesces(t *testing.T) {
	batch := []string{
		"[event: worker-idle]\nWorker: jv-a\nbody-old",
		"[event: worker-idle]\nWorker: jv-b\n",
	}
	arrived := []string{
		"[event: worker-idle]\nWorker: jv-a\nbody-new",
	}
	got := requeueNotifyFront(batch, arrived)
	if len(got) != 2 {
		t.Fatalf("want 2 after re-coalesce, got %d: %v", len(got), got)
	}
	// jv-a should be latest body at original index 0.
	if !strings.Contains(got[0], "body-new") {
		t.Fatalf("jv-a not replaced with latest: %v", got)
	}
}

// 🎯T291 hermetic flood oracle: N workers × M idle transitions leave a
// bounded queue (one note per worker) and drain to 0; owner is never mixed
// behind fleet in the delivered prompt; working level is owner-only.
func TestNotifyQueueFloodCoalescesOwnerPriorityAndWorkingLevel(t *testing.T) {
	s := &Server{}
	var delivered []string
	busy := true
	s.notifySender = func(text string) error {
		if busy {
			return fmt.Errorf("grok acp: prompt already in flight")
		}
		delivered = append(delivered, text)
		return nil
	}

	const nWorkers = 8
	const mTransitions = 12
	// Synthetic flood while overseer is mid-fleet-turn.
	s.mu.Lock()
	s.waiting = true
	s.overseerOwnerTurn = false // fleet chew, not owner chrome
	s.mu.Unlock()

	for tIdx := 0; tIdx < mTransitions; tIdx++ {
		for w := 0; w < nWorkers; w++ {
			name := fmt.Sprintf("jv-w%d", w)
			note := fmt.Sprintf(
				"[event: worker-idle] transition=%d\nWorker: %s\nTarget: 🎯T%d\n",
				tIdx, name, w,
			)
			_ = s.SendToOverseer(note)
		}
	}

	if got := len(s.notifyQueue); got != nWorkers {
		t.Fatalf("after flood queue depth=%d want %d (one per worker)", got, nWorkers)
	}
	if s.overseerWorkingLevel() {
		t.Fatal("fleet-only waiting must not light owner working level")
	}

	// Owner message while fleet backlog is non-empty and session busy.
	_ = s.SendToOverseer(userTurnPrefix + "owner mid-flood")
	if !queueHasOwner(s.notifyQueue) {
		t.Fatal("owner must remain pending while session busy")
	}
	// Depth still bounded: N workers + 1 owner (not N*M + 1).
	if got := len(s.notifyQueue); got != nWorkers+1 {
		t.Fatalf("queue with owner depth=%d want %d", got, nWorkers+1)
	}

	// Session frees (fleet turn ends / interrupt settles).
	busy = false
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	s.drainOverseerNotes()

	if len(delivered) != 1 {
		t.Fatalf("want first drain = one owner prompt, got %d: %v", len(delivered), delivered)
	}
	if !strings.HasPrefix(delivered[0], userTurnPrefix) || !strings.Contains(delivered[0], "owner mid-flood") {
		t.Fatalf("owner must drain first alone, got %q", delivered[0])
	}
	if strings.Contains(delivered[0], "worker-idle") {
		t.Fatal("owner batch must not mix fleet notes")
	}
	if !s.overseerWorkingLevel() {
		t.Fatal("after owner drain, working level must be true")
	}
	if got := len(s.notifyQueue); got != nWorkers {
		t.Fatalf("after owner drain fleet still pending depth=%d want %d", got, nWorkers)
	}

	// Owner turn seals → chrome clears even while fleet remains queued.
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	if s.overseerWorkingLevel() {
		t.Fatal("sealed owner turn must clear working level with fleet still pending")
	}

	// Drain fleet backlog → one coalesced batch, queue to 0.
	s.drainOverseerNotes()
	if len(delivered) != 2 {
		t.Fatalf("want second drain = fleet batch, got %d", len(delivered))
	}
	fleet := delivered[1]
	for w := 0; w < nWorkers; w++ {
		want := fmt.Sprintf("Worker: jv-w%d", w)
		if !strings.Contains(fleet, want) {
			t.Fatalf("fleet batch missing %s", want)
		}
	}
	// Latest transition only (coalesced).
	if strings.Contains(fleet, "transition=0") && mTransitions > 1 {
		// body may only have latest; ensure not M copies per worker.
		count := strings.Count(fleet, "Worker: jv-w0")
		if count != 1 {
			t.Fatalf("jv-w0 appears %d times, want 1 coalesced", count)
		}
	}
	if got := len(s.notifyQueue); got != 0 {
		t.Fatalf("queue must drain to 0, got %d", got)
	}
	// Fleet chew does not light owner chrome.
	if s.overseerOwnerTurn {
		t.Fatal("fleet drain must not set overseerOwnerTurn")
	}
	if s.overseerWorkingLevel() {
		t.Fatal("fleet-only in-flight must not report owner working")
	}
}

func TestOverseerWorkingLevelOwnerOnly(t *testing.T) {
	s := &Server{}
	s.mu.Lock()
	s.waiting = true
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	if s.overseerWorkingLevel() {
		t.Fatal("waiting without owner turn must be false (🎯T291)")
	}
	s.mu.Lock()
	s.overseerOwnerTurn = true
	s.mu.Unlock()
	if !s.overseerWorkingLevel() {
		t.Fatal("owner turn + waiting must be true")
	}
}

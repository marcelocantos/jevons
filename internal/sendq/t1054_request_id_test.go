// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// One question has one host identity, even though the queue and each attempt
// have independent ids. A second submission of identical text is not a retry.
func TestT1054RequestIDSurvivesRestartRetryAndUncertainty(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	at := time.Now()
	first, _, err := q.AppendWithRequestID("po", "same text", "host-question-1", at)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := q.AppendWithRequestID("po", "same text", "host-question-2", at)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.RequestID == second.RequestID {
		t.Fatalf("distinct questions collapsed: %+v %+v", first, second)
	}
	q = NewStore(dir)
	attempt, ok, err := q.ClaimFront("po")
	if err != nil || !ok || attempt.RequestID != first.RequestID || attempt.AttemptID == first.RequestID {
		t.Fatalf("claim: %+v ok=%v err=%v", attempt, ok, err)
	}
	if err := q.Resolve("po", attempt, DefinitelyNotSent, "provider declined before write"); err != nil {
		t.Fatal(err)
	}
	q = NewStore(dir)
	retry, ok, err := q.ClaimFront("po")
	if err != nil || !ok || retry.RequestID != first.RequestID || retry.ID != first.ID || retry.AttemptID == attempt.AttemptID {
		t.Fatalf("retry: %+v ok=%v err=%v", retry, ok, err)
	}
	if err := q.Resolve("po", retry, Unverified, "no receipt"); err != nil {
		t.Fatal(err)
	}
	q = NewStore(dir)
	held, err := q.Snapshot("po")
	if err != nil || len(held) != 2 || held[0].State != Uncertain || held[0].RequestID != first.RequestID || held[1].RequestID != second.RequestID {
		t.Fatalf("held/restart: %+v err=%v", held, err)
	}
	next, ok, err := q.ClaimFront("po")
	if err != nil || !ok || next.RequestID != second.RequestID {
		t.Fatalf("flow past uncertainty: %+v ok=%v err=%v", next, ok, err)
	}
}

func TestT1054QuestionCannotBeDigestedOrSuperseded(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	for i := 0; i <= DigestThreshold; i++ {
		if _, _, err := q.Append("po", "routine", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := q.AppendWithRequestID("po", "question", "host-question", now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := q.ClaimDigest("po"); err != nil || ok {
		t.Fatalf("digest collapsed question: ok=%v err=%v", ok, err)
	}
	if _, _, replaced, err := q.AppendSuperseding("po", "new", now, func(Entry) bool { return true }); err != nil || replaced != DigestThreshold+1 {
		t.Fatalf("supersede: replaced=%d err=%v", replaced, err)
	}
	entries, err := q.Snapshot("po")
	if err != nil || len(entries) != 2 || entries[0].RequestID != "host-question" {
		t.Fatalf("question was replaced: %+v err=%v", entries, err)
	}
}

func TestT1054ConsolidationCannotEraseRequestID(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	if _, _, err := q.Append("po", "routine", now); err != nil {
		t.Fatal(err)
	}
	original, _, err := q.AppendWithRequestID("po", "owner question", "host-question", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Consolidate("po", "", "new instruction", "operator", "reviewed", now); err == nil {
		t.Fatal("consolidated away admitted question")
	}
	entries, err := q.Snapshot("po")
	if err != nil || len(entries) != 2 || entries[1].ID != original.ID || entries[1].RequestID != "host-question" {
		t.Fatalf("request identity lost: %+v %v", entries, err)
	}
}

// Duplicate submissions must not reset the original entry's attempt or age,
// including after reopening the durable queue with a fresh Store instance.
func TestT1054DuplicateRequestIDPendingAttemptUncertainRestart(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	now := time.Now().UTC()
	first, _, err := q.AppendWithRequestID("po", "question", "host-id", now)
	if err != nil {
		t.Fatal(err)
	}
	assertDuplicate := func(state DeliveryState) {
		t.Helper()
		for _, variant := range []struct {
			text     string
			conflict bool
		}{{"question", false}, {"different question", true}} {
			_, _, err := q.AppendWithRequestID("po", variant.text, "host-id", now.Add(time.Minute))
			var duplicate *DuplicateRequestIDError
			if !errors.As(err, &duplicate) || duplicate.Conflict != variant.conflict || duplicate.Existing.ID != first.ID || duplicate.Existing.State != state {
				t.Fatalf("state=%s text=%q duplicate=%+v err=%v", state, variant.text, duplicate, err)
			}
		}
		entries, err := q.Snapshot("po")
		if err != nil || len(entries) != 1 || entries[0].ID != first.ID || entries[0].EnqueuedAt != first.EnqueuedAt {
			t.Fatalf("duplicate changed entry: %+v %v", entries, err)
		}
	}
	assertDuplicate(Pending)
	attempt, ok, err := q.ClaimFront("po")
	if err != nil || !ok {
		t.Fatalf("claim: %+v %v", attempt, err)
	}
	q = NewStore(dir) // the in-flight attempt is held after daemon restart
	assertDuplicate(Attempting)
	if err := q.Resolve("po", attempt, Unverified, "unknown transport result"); err != nil {
		t.Fatal(err)
	}
	q = NewStore(dir)
	assertDuplicate(Uncertain)
	entries, err := q.Snapshot("po")
	if err != nil || entries[0].AttemptID != attempt.AttemptID {
		t.Fatalf("attempt rewritten: %+v %v", entries, err)
	}
}

func TestT1054DuplicateRequestIDSupersedingDoesNotRemoveOthers(t *testing.T) {
	q := NewStore(t.TempDir())
	now := time.Now()
	if _, _, err := q.Append("po", "ordinary", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := q.AppendWithRequestID("po", "question", "host-id", now); err != nil {
		t.Fatal(err)
	}
	_, _, replaced, err := q.AppendSupersedingWithRequestID("po", "different", "host-id", now, func(Entry) bool { return true })
	var duplicate *DuplicateRequestIDError
	if !errors.As(err, &duplicate) || !duplicate.Conflict || replaced != 0 {
		t.Fatalf("duplicate=%+v replaced=%d err=%v", duplicate, replaced, err)
	}
	entries, err := q.Snapshot("po")
	if err != nil || len(entries) != 2 || entries[0].Text != "ordinary" {
		t.Fatalf("mutation on conflict: %+v %v", entries, err)
	}
}

func TestT1054ConcurrentDuplicateRequestIDOneEntry(t *testing.T) {
	q := NewStore(t.TempDir())
	const n = 24
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted, duplicates := 0, 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := q.AppendWithRequestID("po", "question", "host-id", time.Now())
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				accepted++
			} else if _, ok := err.(*DuplicateRequestIDError); ok {
				duplicates++
			} else {
				t.Errorf("append: %v", err)
			}
		}()
	}
	wg.Wait()
	entries, err := q.Snapshot("po")
	if err != nil || accepted != 1 || duplicates != n-1 || len(entries) != 1 {
		t.Fatalf("accepted=%d duplicates=%d entries=%+v err=%v", accepted, duplicates, entries, err)
	}
}

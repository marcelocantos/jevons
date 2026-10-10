// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
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

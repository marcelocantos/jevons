// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package workers

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreWorkersAndEvents(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "workers.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	w := &Worker{
		ID:        "w1",
		Task:      "fix the thing",
		Status:    StatusRunning,
		Model:     "grok-4",
		Cwd:       "/tmp",
		StartedAt: time.Now().UTC(),
	}
	if err := st.InsertWorker(w); err != nil {
		t.Fatal(err)
	}
	id, err := st.AppendEvent("w1", "## Progress")
	if err != nil || id == 0 {
		t.Fatalf("AppendEvent: id=%d err=%v", id, err)
	}
	if err := st.SetPolicy("w1", "allow", "safe", "l1-ok", 1, 7); err != nil {
		t.Fatal(err)
	}
	if err := st.Complete("w1", StatusCompleted, "done", 10, 20, 0.01); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get("w1")
	if err != nil || got == nil {
		t.Fatalf("Get: %v %#v", err, got)
	}
	if got.Status != StatusCompleted || got.InputTokens != 10 || got.OutputTokens != 20 {
		t.Fatalf("unexpected worker: %+v", got)
	}
	if got.PolicyDecision != "allow" || got.AuditSeq != 7 {
		t.Fatalf("policy: %+v", got)
	}
	if got.EndedAt == nil {
		t.Fatal("expected ended_at")
	}

	evs, err := st.Events("w1", 0, 10)
	if err != nil || len(evs) != 1 || evs[0].Line != "## Progress" {
		t.Fatalf("events: %v %+v", err, evs)
	}

	list, err := st.List(StatusCompleted, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %+v", err, list)
	}
}

func TestTrackerSSEHub(t *testing.T) {
	tr, err := NewTracker(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	ch := tr.Hub.Subscribe()
	defer tr.Hub.Unsubscribe(ch)

	if err := tr.Start(StartArgs{ID: "a", Task: "t", Model: "m", Cwd: "/x"}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-ch:
		if e.Type != "worker_started" || e.WorkerID != "a" {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for worker_started")
	}

	if err := tr.Progress("a", "hello line"); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-ch:
		if e.Type != "worker_progress" || e.Line != "hello line" {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for progress")
	}

	if err := tr.Finish(FinishArgs{
		ID: "a", Status: StatusCompleted, Outcome: "ok",
		Policy: &PolicyArgs{Decision: "allow", Level: 1, Reason: "l1", AuditSeq: 1},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-ch:
		if e.Type != "worker_completed" || e.PolicyDecision != "allow" {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for complete")
	}

	w, err := tr.Store.Get("a")
	if err != nil || w == nil || w.PolicyDecision != "allow" {
		t.Fatalf("store policy: %v %+v", err, w)
	}
}

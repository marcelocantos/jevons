// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T600: a tool that kicks off long work returns a handle, not a wait.
func TestDispatchReturnsBeforeTheWorkFinishes(t *testing.T) {
	r := newJobRegistry(t.TempDir())
	release := make(chan struct{})
	start := time.Now()
	j := r.start("jevons_slow_cycle", "jevons", func(context.Context) (string, error) {
		<-release
		return "finished at last", nil
	})
	if took := time.Since(start); took > time.Second {
		t.Fatalf("start blocked for %s — that is the bug this replaces", took)
	}
	if j.State != JobRunning {
		t.Fatalf("state = %s, want running", j.State)
	}
	// The handle must tell a model how to ask, or it will invent a way
	// to wait instead.
	handle := FormatJobHandle(j)
	if !strings.Contains(handle, j.ID) || !strings.Contains(handle, "jevons_job") {
		t.Fatalf("handle does not name itself and the query tool:\n%s", handle)
	}
	close(release)
	r.await(j, jobPollMax)
	got, _ := r.get(j.ID)
	if got.State != JobDone || !strings.Contains(FormatJob(got), "finished at last") {
		t.Fatalf("after completion: %+v", got)
	}
}

// The long-poll is a courtesy, not a way back to blocking.
func TestLongPollIsCappedAndOptional(t *testing.T) {
	r := newJobRegistry(t.TempDir())
	j := r.start("jevons_slow_cycle", "", func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	start := time.Now()
	r.await(j, time.Hour) // a caller asking for an hour
	if took := time.Since(start); took > 2*jobPollMax {
		t.Fatalf("await honoured an hour-long wait (%s); the cap is the point", took)
	}
	r.cancel(j.ID)
}

// A job that ignores its context could not be stopped; the whole reason
// the deadline could not save us was handlers that took _ context.Context.
func TestAJobIsCancellableThroughItsHandle(t *testing.T) {
	r := newJobRegistry(t.TempDir())
	j := r.start("jevons_slow_cycle", "", func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !r.cancel(j.ID) {
		t.Fatal("a running job reported itself uncancellable")
	}
	r.await(j, jobPollMax)
	got, _ := r.get(j.ID)
	if got.State != JobFailed {
		t.Fatalf("cancelled job state = %s, want failed", got.State)
	}
}

// A handle that silently resolves to nothing after a restart would be
// worse than the blocking call it replaces: the caller could not tell
// "still running" from "gone".
func TestAHandleLostToARestartSaysSo(t *testing.T) {
	dir := t.TempDir()
	// A record left behind by a previous boot, still marked running.
	rec := Job{ID: "cycle-20260831T000000Z-1", Kind: "jevons_audit_cycle",
		State: JobRunning, Started: time.Now().Add(-time.Hour)}
	body, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rec.ID+".json"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	r := newJobRegistry(dir) // fresh process: nothing in memory
	got, ok := r.get(rec.ID)
	if !ok {
		t.Fatal("a persisted handle was not found after restart")
	}
	if got.State != JobLost {
		t.Fatalf("state = %s, want lost", got.State)
	}
	if !strings.Contains(FormatJob(got), "restarted") {
		t.Fatalf("the report does not explain what happened:\n%s", FormatJob(got))
	}
	// And a handle that never existed is distinguishable from a lost one.
	if _, ok := r.get("cycle-never-existed-9"); ok {
		t.Fatal("an unknown handle was reported as a job")
	}
}

// A finished job survives the restart WITH its result — the record is not
// only an epitaph.
func TestAFinishedJobSurvivesARestartWithItsResult(t *testing.T) {
	dir := t.TempDir()
	r := newJobRegistry(dir)
	j := r.start("jevons_research_cycle", "", func(context.Context) (string, error) {
		return "3 notes updated", nil
	})
	r.await(j, jobPollMax)

	fresh := newJobRegistry(dir)
	got, ok := fresh.get(j.ID)
	if !ok || got.State != JobDone {
		t.Fatalf("after restart: ok=%v %+v", ok, got)
	}
	if !strings.Contains(FormatJob(got), "3 notes updated") {
		t.Fatalf("result did not survive:\n%s", FormatJob(got))
	}
}

// The point of the whole target: no healthy call can outlive the cockpit's
// stuck-busy timer, so a working overseer is never called stuck merely for
// invoking a tool.
func TestTheLongPollCannotOutliveTheStuckTimer(t *testing.T) {
	if jobPollMax >= DefaultFleetStuckTimeout {
		t.Fatalf("long-poll cap %s is not shorter than the stuck timer %s",
			jobPollMax, DefaultFleetStuckTimeout)
	}
}

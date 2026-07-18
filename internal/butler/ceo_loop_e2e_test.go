// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package butler_test

// 🎯T46: the CEO-loop oracle. One hermetic scenario chains the full
// lifecycle — adopt → two-writer-guarded take-over → direct → reap-idle
// → no-silent-fail rehydrate → restart-with-history — over the real
// store/scanner/reader and a fake fleet. The stage-level tests elsewhere
// in this package cover each transition in isolation; this test exists
// so a regression anywhere in the chain fails ONE readable scenario.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
)

func TestCEOLoopScenario(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	storePath := filepath.Join(dir, "threads.json")
	writeOwnerTranscript(t, sessionsDir)

	f := newFakeFleet()
	externallyActive := true // the owner is still driving the session
	now := fixedNow          // advanced mid-scenario to age the thread into idleness

	store, err := thread.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	b := butler.New(butler.Config{
		Store:            store,
		Scanner:          discovery.NewScanner(sessionsDir),
		Reader:           transcript.NewReader(sessionsDir),
		Fleet:            f,
		Now:              func() time.Time { return now },
		ExternallyActive: func(string) bool { return externallyActive },
	})

	// ADOPT (observe-only): non-invasive, immediately visible.
	th, err := b.Adopt(butler.AdoptArgs{SessionID: ownerSession, ID: "po", ObserveOnly: true})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if th.Kind != thread.KindAdopted {
		t.Fatalf("adopted thread kind = %v, want observe-only adopted", th.Kind)
	}

	// Observe-only threads must refuse Direct loudly (no silent second writer).
	if _, err := b.Direct("po", "do something"); err == nil {
		t.Fatal("Direct on an observe-only thread must refuse")
	}

	// TWO-WRITER GUARD: take-over while the owner still drives the session
	// is refused; the observe-only record survives.
	if _, err := b.TakeOver("po"); err == nil {
		t.Fatal("TakeOver must refuse while the session is externally active")
	}
	if got, ok := store.Get("po"); !ok || got.Kind != thread.KindAdopted {
		t.Fatalf("refused take-over corrupted the thread record: %+v", got)
	}

	// Owner stops driving it — take-over now succeeds and launches our
	// process resuming the same session.
	externallyActive = false
	th, err = b.TakeOver("po")
	if err != nil {
		t.Fatalf("TakeOver after release: %v", err)
	}
	if th.Kind != thread.KindSpawned || th.SessionID != ownerSession {
		t.Fatalf("take-over lost identity: %+v", th)
	}

	// DIRECT: the turn round-trips.
	reply, err := b.Direct("po", "ship the fix")
	if err != nil {
		t.Fatalf("Direct: %v", err)
	}
	if !strings.Contains(reply, "ship the fix") {
		t.Fatalf("directed turn did not round-trip: %q", reply)
	}

	// REAP-IDLE (process-as-cache): age the thread past the idle threshold
	// — its process is stopped, the thread stays durable.
	now = fixedNow.Add(thread.DefaultIdleThreshold + time.Minute)
	reaped := b.ReapIdle()
	if len(reaped) != 1 || reaped[0] != "po" {
		t.Fatalf("ReapIdle = %v, want [po]", reaped)
	}
	if f.Alive("po") {
		t.Fatal("reap left the process alive")
	}

	// NO SILENT-FAIL: with the process gone AND unlaunchable, Direct must
	// return an actionable error, never a silent void.
	f.launchErr = fmt.Errorf("tmux server gone")
	if _, err := b.Direct("po", "hello?"); err == nil || !strings.Contains(err.Error(), "rehydrate") {
		t.Fatalf("Direct with dead+unlaunchable process: err = %v, want rehydrate failure", err)
	}

	// Transparent REHYDRATE once the process can start again.
	f.launchErr = nil
	launchesBefore := f.launches
	if _, err := b.Direct("po", "still there?"); err != nil {
		t.Fatalf("Direct after reap: %v", err)
	}
	if f.launches != launchesBefore+1 {
		t.Fatalf("Direct did not rehydrate: launches = %d, want %d", f.launches, launchesBefore+1)
	}

	// RESTART-WITH-HISTORY: a fresh butler over the same store (daemon
	// restart) still has the thread, same session id, and can read its
	// history from the transcript — the durable unit outlives everything.
	store2, err := thread.NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore (restart): %v", err)
	}
	b2 := butler.New(butler.Config{
		Store:            store2,
		Scanner:          discovery.NewScanner(sessionsDir),
		Reader:           transcript.NewReader(sessionsDir),
		Fleet:            newFakeFleet(),
		Now:              func() time.Time { return fixedNow },
		ExternallyActive: func(string) bool { return false },
	})
	st, err := b2.Status("po")
	if err != nil {
		t.Fatalf("Status after restart: %v", err)
	}
	if st.Thread.SessionID != ownerSession {
		t.Fatalf("restart lost the session id: %+v", st.Thread)
	}
	if !strings.Contains(st.Status.Summary, "recursive backtracking") {
		t.Fatalf("restart lost history: status = %+v", st.Status)
	}
}

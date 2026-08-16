// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The whole point: two concurrent runs must not overlap. The restart storm
// on 2026-08-09 was two invocations 59s apart, the second killing the first
// mid-flight and leaving the daemon down.
//
// The waiter is given no deadline (🎯T400). This test asserts serialisation,
// not promptness, and a deadline would quietly add a second assertion — that
// the machine can fork a shell and run two printfs inside it within N seconds
// — which is false on a loaded machine and has nothing to do with the lock.
// It was: under a deliberate 24-way load this failed ten runs in twenty-four
// with "run B exited 75", the EX_TEMPFAIL of a waiter that gave up on a lock
// that was working exactly as specified. A test that fails for reasons
// unrelated to its subject teaches everyone to discount its output. If the
// lock ever genuinely deadlocks, `go test`'s own timeout ends this with a
// goroutine dump, which names the wedged waiter rather than hiding it behind
// an exit code.
func TestConcurrentRunsDoNotOverlap(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "run.lock")
	marker := filepath.Join(dir, "trace")

	// Each run appends its start, sleeps, then appends its end. Overlap
	// shows up as interleaved markers.
	script := filepath.Join(dir, "work.sh")
	body := "#!/bin/sh\nprintf 'start%s\\n' \"$1\" >>" + marker +
		"\nsleep 0.4\nprintf 'end%s\\n' \"$1\" >>" + marker + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for _, id := range []string{"A", "B"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if code := run([]string{"-quiet", "-timeout", "0", lock, script, id}); code != 0 {
				t.Errorf("run %s exited %d", id, code)
			}
		}(id)
		time.Sleep(50 * time.Millisecond) // stagger, as the storm did
	}
	wg.Wait()

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(got))
	if len(lines) != 4 {
		t.Fatalf("expected 4 markers, got %v", lines)
	}
	// Serialised means each start is immediately followed by its own end.
	for i := 0; i < len(lines); i += 2 {
		s, e := lines[i], lines[i+1]
		if !strings.HasPrefix(s, "start") || !strings.HasPrefix(e, "end") ||
			strings.TrimPrefix(s, "start") != strings.TrimPrefix(e, "end") {
			t.Fatalf("runs overlapped: %v", lines)
		}
	}
}

// A waiter must not silently skip: the second run still executes, because a
// change that landed after the first began would otherwise never activate.
func TestWaiterStillRuns(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "run.lock")
	out := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "w.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf x >>"+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if code := run([]string{"-quiet", lock, script}); code != 0 {
			t.Fatalf("run %d exited %d", i, code)
		}
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "xxx" {
		t.Fatalf("ran %q times, want all three", string(b))
	}
}

// 🎯T400: -timeout 0 outwaits a holder for as long as it holds. A caller that
// wants mutual exclusion and nothing else must be able to say so, or it is
// stuck encoding a guess about machine speed into a constant.
func TestZeroTimeoutOutwaitsTheHolder(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "run.lock")
	out := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "w.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf x >>"+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	held, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("could not take the lock to hold it: %v", err)
	}

	code := make(chan int, 1)
	go func() { code <- run([]string{"-quiet", "-timeout", "0", lock, script}) }()

	// Confirm it is still waiting rather than already through. Polls only
	// strengthen this check: extra ones make a waiter that wrongly gave up
	// easier to catch, and none of them can fail a waiter that is behaving.
	for i := 0; i < 20; i++ {
		select {
		case c := <-code:
			t.Fatalf("waiter returned %d while the lock was held", c)
		case <-time.After(20 * time.Millisecond):
		}
	}

	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if c := <-code; c != 0 {
		t.Fatalf("waiter exited %d after the lock was released", c)
	}
	b, err := os.ReadFile(out)
	if err != nil || string(b) != "x" {
		t.Fatalf("waited but never ran: %q %v", string(b), err)
	}
}

// The finite timeout still gives up, or "wait indefinitely" would have been
// bought by disabling the wedged-holder escape hatch for every caller.
func TestFiniteTimeoutStillGivesUp(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "run.lock")
	script := filepath.Join(dir, "w.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("could not take the lock to hold it: %v", err)
	}
	if code := run([]string{"-quiet", "-timeout", "50ms", lock, script}); code != 75 {
		t.Fatalf("exit=%d want 75 (EX_TEMPFAIL) when the holder outlasts the timeout", code)
	}
}

// The wrapped command's exit status is the wrapper's exit status, or a
// failing restart would report success to the fleet.
func TestExitCodePropagates(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 17\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-quiet", filepath.Join(dir, "l"), script}); code != 17 {
		t.Fatalf("exit=%d want 17", code)
	}
}

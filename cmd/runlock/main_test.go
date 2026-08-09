// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The whole point: two concurrent runs must not overlap. The restart storm
// on 2026-08-09 was two invocations 59s apart, the second killing the first
// mid-flight and leaving the daemon down.
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
			if code := run([]string{"-quiet", "-timeout", "10s", lock, script, id}); code != 0 {
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

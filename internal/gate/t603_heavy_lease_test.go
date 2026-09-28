// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// HeavyCommand classifier (acceptance: N seats each running
// `go test ./internal/mcpserver` queue rather than race).
func TestT603HeavyCommandClassifier(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want bool
	}{
		{"go test package", []string{"go", "test", "./internal/mcpserver"}, true},
		{"go test dots", []string{"go", "test", "./..."}, true},
		{"go build not test", []string{"go", "build", "./..."}, false},
		{"go vet not test", []string{"go", "vet", "./..."}, false},
		{"make test-go", []string{"make", "test-go"}, true},
		{"make ui-build", []string{"make", "ui-build"}, false},
		{"sh not heavy", []string{"sh", "-c", "go test ./..."}, false},
		{"empty argv", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := HeavyCommand(tc.argv); got != tc.want {
				t.Fatalf("HeavyCommand(%v) = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}
}

// A second lease acquisition waits for the first to release rather than
// starting concurrently — the queue, not the race.
func TestT603HeavyLeaseSerialisesConcurrentHolders(t *testing.T) {
	root := t.TempDir()

	release1, err := AcquireHeavyLease(root)
	if err != nil {
		t.Fatalf("AcquireHeavyLease #1: %v", err)
	}

	started := make(chan struct{})
	acquired := make(chan time.Time, 1)
	go func() {
		close(started)
		release2, err := AcquireHeavyLease(root)
		if err != nil {
			t.Error(err)
			return
		}
		acquired <- time.Now()
		release2()
	}()
	<-started
	// Give the second goroutine a real chance to (wrongly) acquire the lease
	// while the first still holds it.
	time.Sleep(150 * time.Millisecond)

	select {
	case <-acquired:
		t.Fatal("second AcquireHeavyLease returned while the first still held the lease")
	default:
	}

	releasedAt := time.Now()
	release1()

	var gotAt time.Time
	select {
	case gotAt = <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("second AcquireHeavyLease never returned after release")
	}
	if gotAt.Before(releasedAt) {
		t.Fatalf("second lease acquired at %v, before release at %v", gotAt, releasedAt)
	}
}

// Run() itself takes the lease for a heavy command: two concurrent Run
// calls sharing a store, both classified heavy, do not overlap.
func TestT603RunSerialisesConcurrentHeavyRuns(t *testing.T) {
	dir := t.TempDir()
	mk := filepath.Join(dir, "heavy.mk")
	// A fake "test" target that sleeps, so this test stays fast and does not
	// depend on the real go toolchain's own weight — the classifier is what
	// is under test here, not go test's actual runtime.
	if err := os.WriteFile(mk, []byte("test-heavy:\n\tsleep 0.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	var mu sync.Mutex
	var windows []([2]time.Time)
	runOne := func(name string) {
		start := time.Now()
		_, err := Run(&RunArgs{
			Command: []string{"make", "-f", mk, "test-heavy"},
			Name:    name,
			Store:   store,
			Stdout:  io.Discard,
			Stderr:  io.Discard,
		})
		end := time.Now()
		if err != nil {
			t.Errorf("Run %s: %v", name, err)
			return
		}
		mu.Lock()
		windows = append(windows, [2]time.Time{start, end})
		mu.Unlock()
	}

	suiteStart := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			runOne(fmt.Sprintf("t603-heavy-%d", i))
		}()
	}
	wg.Wait()
	elapsed := time.Since(suiteStart)

	if len(windows) != 2 {
		t.Fatalf("got %d completed runs, want 2", len(windows))
	}
	// Each make target sleeps 0.3s. Raced (unserialised), two goroutines
	// running concurrently finish in ~0.3s total. Serialised via the lease,
	// the second waits for the first: total wall time is close to 2×0.3s.
	// The whole-Run() windows recorded above legitimately overlap even when
	// serialised (the second call is inside Run(), blocked on the lease,
	// for the entire time it waits) — total elapsed is the signal that
	// distinguishes queued from raced, not window overlap.
	const perRun = 300 * time.Millisecond
	if elapsed < perRun*3/2 {
		t.Fatalf("two heavy runs finished in %v, want >= %v if the lease serialised them "+
			"(windows: %v..%v and %v..%v)",
			elapsed, perRun*3/2, windows[0][0], windows[0][1], windows[1][0], windows[1][1])
	}
}

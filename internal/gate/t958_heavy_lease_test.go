// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestT958NarrowCommandClassifier(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		want bool
	}{
		{"go test single pkg", []string{"go", "test", "./internal/gate"}, true},
		{"go test -run subset", []string{"go", "test", "-run", "TestFoo", "./..."}, true},
		{"go test full dots", []string{"go", "test", "./..."}, false},
		{"go test two pkgs", []string{"go", "test", "./internal/gate", "./internal/plan"}, false},
		{"make test-go full", []string{"make", "test-go"}, false},
		{"not heavy", []string{"go", "build", "./..."}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NarrowCommand(tc.argv); got != tc.want {
				t.Fatalf("NarrowCommand(%v) = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}
}

func TestT958DuplicateWaiterRefused(t *testing.T) {
	root := t.TempDir()
	cmd := []string{"go", "test", "-run", "TestNone", "./internal/gate"}
	cwd := t.TempDir()

	release, err := AcquireHeavyLease(root)
	if err != nil {
		t.Fatal(err)
	}
	registerHeavyWaiter(root, HeavyLeaseWaiter{
		PID:     os.Getpid(),
		Command: cmd,
		CWD:     cwd,
		Started: time.Now(),
		Narrow:  true,
	})
	_, _, err = acquireHeavyLeaseCommand(root, "", cmd, cwd)
	if err == nil || !strings.Contains(err.Error(), "refusing duplicate") {
		t.Fatalf("want duplicate refusal, got %v", err)
	}
	release()
}

func TestT958WaitMessageNamesHolder(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	heavyLeaseWaitOut = &buf
	defer func() { heavyLeaseWaitOut = os.Stderr }()

	holder, _, err := acquireHeavyLeaseCommand(root, "", []string{"go", "test", "./..."}, "/holder/cwd")
	if err != nil {
		t.Fatal(err)
	}
	defer holder()

	done := make(chan struct{})
	go func() {
		_, _, err := acquireHeavyLeaseCommand(root, "", []string{"go", "test", "-run", "TestX", "./internal/gate"}, "/waiter/cwd")
		if err != nil {
			t.Error(err)
		}
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	holder()
	<-done

	out := buf.String()
	if !strings.Contains(out, "holder pid=") || !strings.Contains(out, `"go" "test" "./..."`) || !strings.Contains(out, `cwd="/holder/cwd"`) {
		t.Fatalf("wait output missing holder fields: %q", out)
	}
}

func TestT958NarrowBypassesStaleFullHolder(t *testing.T) {
	root := t.TempDir()
	heavyNarrowBypassAge = 20 * time.Millisecond
	heavyWaitPoll = 5 * time.Millisecond
	defer func() {
		heavyNarrowBypassAge = 5 * time.Minute
		heavyWaitPoll = 100 * time.Millisecond
	}()

	holderRelease, _, err := acquireHeavyLeaseCommand(root, "", []string{"go", "test", "./..."}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(heavyLockPath(root) + ".owner")
	if err != nil {
		t.Fatal(err)
	}
	var owner HeavyLeaseOwner
	if err := json.Unmarshal(data, &owner); err != nil {
		t.Fatal(err)
	}
	owner.Started = time.Now().Add(-time.Minute)
	if err := writeAtomic(heavyLockPath(root)+".owner", mustJSON(owner), 0o600); err != nil {
		t.Fatal(err)
	}

	acquired := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		release, _, err := acquireHeavyLeaseCommand(root, "", []string{"go", "test", "-run", "TestNone", "./internal/gate"}, t.TempDir())
		if err != nil {
			t.Error(err)
			return
		}
		release()
		acquired <- time.Since(start)
	}()

	select {
	case waited := <-acquired:
		if waited > 200*time.Millisecond {
			t.Fatalf("narrow bypass took %v; expected prompt return while holder still runs", waited)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("narrow acquire blocked too long")
	}
	holderRelease()
}

func TestT958TwoFullSuitesStillSerialize(t *testing.T) {
	root := t.TempDir()
	heavyWaitPoll = 5 * time.Millisecond
	defer func() { heavyWaitPoll = 100 * time.Millisecond }()

	var mu sync.Mutex
	var holds []time.Time
	var releases []time.Time

	run := func() {
		release, _, err := acquireHeavyLeaseCommand(root, "", []string{"go", "test", "./..."}, t.TempDir())
		if err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		holds = append(holds, time.Now())
		mu.Unlock()
		time.Sleep(80 * time.Millisecond)
		end := time.Now()
		release()
		mu.Lock()
		releases = append(releases, end)
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); run() }()
	}
	wg.Wait()
	if len(holds) != 2 || len(releases) != 2 {
		t.Fatalf("expected 2 runs, got holds=%d releases=%d", len(holds), len(releases))
	}
	if holds[0].After(holds[1]) {
		holds[0], holds[1] = holds[1], holds[0]
		releases[0], releases[1] = releases[1], releases[0]
	}
	if holds[1].Before(releases[0]) {
		t.Fatalf("full suites overlapped: holds=%v releases=%v", holds, releases)
	}
}

func TestT958StaleNoticeOncePerHolder(t *testing.T) {
	root := t.TempDir()
	heavyStaleNoticeAge = 1 * time.Millisecond
	defer func() { heavyStaleNoticeAge = 5 * time.Minute }()

	release, _, err := acquireHeavyLeaseCommand(root, "", []string{"go", "test", "./..."}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(heavyLockPath(root) + ".owner")
	var owner HeavyLeaseOwner
	_ = json.Unmarshal(data, &owner)
	owner.Started = time.Now().Add(-time.Minute)
	_ = writeAtomic(heavyLockPath(root)+".owner", mustJSON(owner), 0o600)

	registerHeavyWaiter(root, HeavyLeaseWaiter{PID: 99999, Command: []string{"go", "test", "-run", "X"}, CWD: "/w", Started: time.Now(), Narrow: true})
	maybeStaleNotice(root, &owner, 1)
	maybeStaleNotice(root, &owner, 2)
	notice, err := os.ReadFile(heavyStaleNoticePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(notice), "heavy lease stale:") != 1 {
		t.Fatalf("want one stale notice, got %q", notice)
	}
	release()
}

func TestT958QueueShowsWaiters(t *testing.T) {
	root := t.TempDir()
	release, _, err := acquireHeavyLeaseCommand(root, "", []string{"go", "test", "./..."}, "/holder")
	if err != nil {
		t.Fatal(err)
	}
	waiterDone := make(chan struct{})
	go func() {
		_, _, err := acquireHeavyLeaseCommand(root, "", []string{"go", "test", "-run", "TestY", "./internal/gate"}, "/waiter")
		if err != nil {
			t.Error(err)
		}
		close(waiterDone)
	}()
	time.Sleep(30 * time.Millisecond)
	var out bytes.Buffer
	if err := WriteHeavyLeaseStatus(&out, root); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "waiters: 1") || !strings.Contains(s, "waiter pid=") {
		t.Fatalf("missing waiter listing: %s", s)
	}
	release()
	<-waiterDone
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

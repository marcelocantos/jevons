// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T1219: alternating frontier polls for two workdirs must not churn watchers
// or re-seed each other's achieve state.

func twoLedgerFrontierFixture(t *testing.T) (repoA, repoB, ledgerA, ledgerB string) {
	t.Helper()
	root := t.TempDir()
	body := "schema_version: 1\ntargets:\n  T1:\n    name: leaf\n    status: converging\n"
	mk := func(name string) (string, string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		led := filepath.Join(dir, "bullseye.yaml")
		if err := os.WriteFile(led, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir, led
	}
	repoA, ledgerA = mk("jevons")
	repoB, ledgerB = mk("jv-t709-auto")
	return repoA, repoB, ledgerA, ledgerB
}

func mockTwoLedgerBullseyeCLI(repoA, repoB, ledgerA, ledgerB string) {
	runBullseyeCLI = func(args ...string) (string, error) {
		for i, a := range args {
			if a == "--cwd" && i+1 < len(args) {
				cwd := args[i+1]
				if cwd == repoA {
					return "File: " + ledgerA + "\n", nil
				}
				if cwd == repoB {
					return "File: " + ledgerB + "\n", nil
				}
			}
		}
		return "", nil
	}
}

func pollFrontierHTTP(t *testing.T, s *Server, cwd string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/frontier?cwd="+cwd, nil)
	rr := httptest.NewRecorder()
	s.handleFrontier(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("frontier cwd=%s status %d body=%s", cwd, rr.Code, rr.Body.String())
	}
	var resp FrontierResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Available {
		t.Fatalf("frontier cwd=%s unavailable: %+v", cwd, resp)
	}
}

func attachFrontierChangedListener(t *testing.T, s *Server) chan string {
	t.Helper()
	ch := make(chan string, 8)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	return ch
}

// frontierEventWait bounds an fsnotify delivery. It is a deadline, not a
// sleep: 2s timed out at host load ~120 in a post-merge gate (2026-10-28).
const frontierEventWait = 10 * time.Second

func waitFrontierChanged(t *testing.T, ch chan string, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case line := <-ch:
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatal(err)
			}
			if m["type"] == "frontier_changed" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for frontier_changed")
		}
	}
}

func assertNoFrontierChanged(t *testing.T, ch chan string, within time.Duration) {
	t.Helper()
	select {
	case line := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] == "frontier_changed" {
			t.Fatalf("unexpected frontier_changed: %s", line)
		}
	case <-time.After(within):
	}
}

func TestT1219AlternatingFrontierPollsKeepBoundedWatchers(t *testing.T) {
	prevCLI := runBullseyeCLI
	prevHook := frontierWatchObserved
	t.Cleanup(func() {
		runBullseyeCLI = prevCLI
		frontierWatchObserved = prevHook
	})

	repoA, repoB, ledgerA, ledgerB := twoLedgerFrontierFixture(t)
	mockTwoLedgerBullseyeCLI(repoA, repoB, ledgerA, ledgerB)

	var (
		mu     sync.Mutex
		starts map[string]int
	)
	starts = make(map[string]int)
	frontierWatchObserved = func(ledger string) {
		mu.Lock()
		starts[ledger]++
		mu.Unlock()
	}

	s := New("test", t.TempDir())
	for i := 0; i < 20; i++ {
		cwd := repoA
		if i%2 == 1 {
			cwd = repoB
		}
		pollFrontierHTTP(t, s, cwd)
	}

	mu.Lock()
	defer mu.Unlock()
	if starts[ledgerA] != 1 || starts[ledgerB] != 1 {
		t.Fatalf("watch starts per ledger want 1 each, got %v", starts)
	}
	s.mu.Lock()
	n := len(s.frontierWatches)
	s.mu.Unlock()
	if n != 2 {
		t.Fatalf("active frontier watches=%d want 2", n)
	}
}

func TestT1219AchieveStateNotReseededByOtherLedgerPoll(t *testing.T) {
	repoA, repoB, ledgerA, ledgerB := twoLedgerFrontierFixture(t)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jv-a", WorkDir: repoA, SessionID: "s-a", Purpose: claudia.PurposeWork,
			Parent: "po-a", Materialized: true, Provider: "grok", TargetID: "T1"},
		{Name: "jv-b", WorkDir: repoB, SessionID: "s-b", Purpose: claudia.PurposeWork,
			Parent: "po-b", Materialized: true, Provider: "grok", TargetID: "T1"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}

	s := &Server{registry: reg, overseerName: "jevons"}
	s.ensureFrontierWatch(ledgerA)
	s.ensureFrontierWatch(ledgerB)

	// Simulate open state on A while B still has no achieves recorded.
	s.mu.Lock()
	if s.achievedTargetsSeen == nil {
		s.achievedTargetsSeen = make(map[string]map[string]bool)
	}
	s.achievedTargetsSeen[ledgerA] = map[string]bool{}
	s.achievedTargetsSeen[ledgerB] = map[string]bool{}
	s.mu.Unlock()

	achieved := "schema_version: 1\ntargets:\n  T1:\n    name: leaf\n    status: achieved\n"
	if err := os.WriteFile(ledgerA, []byte(achieved), 0o644); err != nil {
		t.Fatal(err)
	}

	// Polling B used to rebind the global watcher and re-seed A's achieve set.
	s.ensureFrontierWatch(ledgerB)

	s.maybeReapOnLedgerAchieve(ledgerA)
	if reg.Def("jv-a") != nil {
		t.Fatal("achieve on ledger A must reap its worker")
	}
	if reg.Def("jv-b") == nil {
		t.Fatal("ledger B worker must survive A's achieve")
	}
}

// Both ledgers started via HTTP frontier polls must independently receive
// fsnotify-driven frontier_changed frames when their yaml is written.
func TestT1219BothLedgersReceiveFsnotifyNotifications(t *testing.T) {
	prevCLI := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = prevCLI })

	repoA, repoB, ledgerA, ledgerB := twoLedgerFrontierFixture(t)
	mockTwoLedgerBullseyeCLI(repoA, repoB, ledgerA, ledgerB)

	s := New("test", t.TempDir())
	pollFrontierHTTP(t, s, repoA)
	pollFrontierHTTP(t, s, repoB)

	ch := attachFrontierChangedListener(t, s)
	touch := "schema_version: 1\ntargets:\n  T1:\n    name: leaf\n    status: converging\n    context: touch\n"

	if err := os.WriteFile(ledgerA, []byte(touch), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFrontierChanged(t, ch, frontierEventWait)

	if err := os.WriteFile(ledgerB, []byte(touch), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFrontierChanged(t, ch, frontierEventWait)
}

// Stopping one ledger's watch must not cancel the other's fsnotify goroutine.
func TestT1219WatchStopScopePerLedger(t *testing.T) {
	prevCLI := runBullseyeCLI
	t.Cleanup(func() { runBullseyeCLI = prevCLI })

	repoA, repoB, ledgerA, ledgerB := twoLedgerFrontierFixture(t)
	mockTwoLedgerBullseyeCLI(repoA, repoB, ledgerA, ledgerB)

	s := New("test", t.TempDir())
	pollFrontierHTTP(t, s, repoA)
	pollFrontierHTTP(t, s, repoB)

	stopA := s.WatchFrontierFile(ledgerA)
	stopA()

	s.mu.Lock()
	_, okA := s.frontierWatches[ledgerA]
	_, okB := s.frontierWatches[ledgerB]
	s.mu.Unlock()
	if okA {
		t.Fatal("ledger A watch must be removed after stop")
	}
	if !okB {
		t.Fatal("ledger B watch must remain after stopping A")
	}

	ch := attachFrontierChangedListener(t, s)
	touch := "schema_version: 1\ntargets:\n  T1:\n    name: leaf\n    status: converging\n    context: touch\n"

	if err := os.WriteFile(ledgerA, []byte(touch), 0o644); err != nil {
		t.Fatal(err)
	}
	assertNoFrontierChanged(t, ch, 400*time.Millisecond)

	if err := os.WriteFile(ledgerB, []byte(touch), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFrontierChanged(t, ch, frontierEventWait)
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T400: the standing gate is deterministic — no test fails only because
// the machine is busy.
//
// Two tests failed under a loaded full-suite run and passed in isolation:
// cmd/runlock TestConcurrentRunsDoNotOverlap and internal/audit
// TestScheduledCycleDefersUnderCapacityPressure. Both are concurrency-control
// code, so widening a deadline would have papered over the very property
// they exist to assert. Both were fixed at the root instead — the runlock
// waiter gained a real "no deadline" mode and the audit schedule gained an
// injectable tick — and this file is the ratchet that keeps them there.
//
// What this ratchet is, precisely: the offenders run under deliberate
// concurrent load rather than in isolation, and every one of them must be
// observed to PASS. Skipping is a failure, because the cheapest way to make
// a load-sensitive test stop failing under load is to stop running it under
// load, and that fix would otherwise be indistinguishable from a real one.
//
// What this ratchet is NOT: a reproduction of the original flake. The
// pre-fix failure was statistical — ten runs in twenty-four under a
// deliberate 24-way load — so a control that demanded red would itself be
// flaky, which is the disease rather than the cure. The red-against-pre-fix
// evidence therefore lives in two deterministic places instead:
//
//   - cmd/runlock TestFiniteTimeoutStillGivesUp reproduces the exact pre-fix
//     failure with no load at all: a waiter under a finite deadline exits 75
//     (EX_TEMPFAIL) when the holder outlives it. Its sibling
//     TestZeroTimeoutOutwaitsTheHolder runs the identical scenario with the
//     fix and passes. Each is the other's control, and neither depends on how
//     busy the machine is.
//   - internal/audit TestScheduledCycleDefersUnderCapacityPressure pins
//     Interval to an hour beside the injected tick, so a regression that
//     ignored the injection would hang on the rendezvous rather than pass on
//     a stray real tick.
//
// The against-the-pre-fix-tree run was done by hand over a worktree of the
// pre-fix commit, under this same load harness; t400Offenders below is the
// set it named.
package docratchet_test

import (
	"encoding/json"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The offenders named in 🎯T400 acceptance clause 2, keyed by the package
// they live in. Both must be observed passing under load, by name: a rename
// or a deletion fails this ratchet as loudly as a red run.
var t400Offenders = map[string][]string{
	"./cmd/runlock/":    {"TestConcurrentRunsDoNotOverlap"},
	"./internal/audit/": {"TestScheduledCycleDefersUnderCapacityPressure"},
}

// t400Repeats is how many times each offender runs under load. The original
// flake showed at roughly four runs in ten, so a handful of repetitions is
// the difference between exercising the property and sampling it once.
const t400Repeats = 5

// TestT400OffendersStayGreenUnderDeliberateLoad is the ratchet. It saturates
// the machine, then runs the offenders repeatedly against that.
func TestT400OffendersStayGreenUnderDeliberateLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("load ratchet is not a -short test")
	}
	root := repoRoot(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}

	stop := t400Load(t)
	defer stop()

	for pkg, tests := range t400Offenders {
		filter := "^(" + strings.Join(tests, "|") + ")$"
		cmd := exec.Command("go", "test", "-json", "-count", strconv.Itoa(t400Repeats), "-run", filter, pkg)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()

		ran, skipped, failed := t400Verdicts(string(out))
		for _, name := range tests {
			switch {
			case skipped[name] > 0:
				// The over-broadness check. A load-sensitive test that
				// stops failing under load because it stopped running
				// under load is not fixed, and this is the one shape of
				// "green" that must not be accepted as one.
				t.Errorf("%s %s was SKIPPED under load (%d of %d runs).\n"+
					"🎯T400 is not satisfied by declining to run the test that used to fail;\n"+
					"fix the wall-clock dependency at the root, as the runlock waiter and\n"+
					"the audit schedule were.", pkg, name, skipped[name], t400Repeats)
			case failed[name] > 0:
				t.Errorf("%s %s failed %d of %d runs under load — the very flake 🎯T400 closed.\n%s",
					pkg, name, failed[name], t400Repeats, t400Tail(string(out)))
			case ran[name] != t400Repeats:
				t.Errorf("%s %s was observed passing %d times, want %d.\n"+
					"A named offender that no longer runs cannot be shown to be fixed.\n%s",
					pkg, name, ran[name], t400Repeats, t400Tail(string(out)))
			}
		}
		if err != nil && !t.Failed() {
			// The package went red somewhere other than the offenders —
			// still a red gate, and still worth naming here rather than
			// swallowing because the tests we asked about were fine.
			t.Errorf("go test %s failed under load for another reason: %v\n%s",
				pkg, err, t400Tail(string(out)))
		}
	}
}

// t400Load saturates the machine for as long as the returned stop is
// uncalled. Spinners rather than forked processes: the subject runs in its
// own `go test` subprocess, so contention for CPU is what has to be real,
// and in-process spinners make that contention without a second pile of
// process lifetimes to get wrong.
//
// The returned stop reports how many spin iterations were completed, so a
// harness that silently failed to load the machine is visible rather than
// quietly making this ratchet vacuous.
func t400Load(t *testing.T) func() {
	t.Helper()
	n := runtime.NumCPU() * 3
	done := make(chan struct{})
	var spins atomic.Uint64
	for i := 0; i < n; i++ {
		go func() {
			var x uint64
			for {
				select {
				case <-done:
					return
				default:
				}
				for j := 0; j < 1_000_000; j++ {
					x = x*1664525 + 1013904223
				}
				spins.Add(1)
				_ = x
			}
		}()
	}
	start := time.Now()
	return func() {
		close(done)
		got := spins.Load()
		t.Logf("load: %d spinners, %d iterations over %s", n, got, time.Since(start).Round(time.Millisecond))
		if got == 0 {
			t.Error("the load generator never completed a single spin — this ratchet ran the offenders idle, " +
				"which is exactly the isolated condition 🎯T400 says proves nothing")
		}
	}
}

// t400Verdicts counts pass/skip/fail per test name from `go test -json`.
// Reading the machine-readable stream rather than scraping prose is what
// makes "was it skipped?" answerable at all: a skipped test and an absent
// one look identical in the default output.
func t400Verdicts(stream string) (pass, skip, fail map[string]int) {
	pass, skip, fail = map[string]int{}, map[string]int{}, map[string]int{}
	for _, line := range strings.Split(stream, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue // build errors and stray output are not events
		}
		var ev struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Test == "" {
			continue
		}
		switch ev.Action {
		case "pass":
			pass[ev.Test]++
		case "skip":
			skip[ev.Test]++
		case "fail":
			fail[ev.Test]++
		}
	}
	return pass, skip, fail
}

// t400Tail keeps a failure message readable: -json output is one object per
// line and dumping all of it buries the reason.
func t400Tail(stream string) string {
	lines := strings.Split(strings.TrimSpace(stream), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return strings.Join(lines, "\n")
}

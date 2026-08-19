// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 🎯T450 — TestRestartThrashPolicy renders a verdict on the tree; a loaded
// machine cannot turn it red.
//
// Sibling of 🎯T442 (injected date shim in t218_restart_thrash_test.go) and
// 🎯T437 (owner-health clock injection). T442 owns the mechanism: the thrash
// window is arithmetic on an injected clock, never wall time between two
// script runs. This file owns the load + mutation net that keeps that
// mechanism honest:
//
//  1. Under the T437 load shape (32 busy loops), the thrash oracle stays green.
//  2. A tree whose script skips the wait still fails the wait assertion.
//  3. A tree that claims OK while leaving the old binary serving still fails
//     the stale-binary assertion.
//
// Widening MIN_INTERVAL_SEC is not a fix — it only lowers the firing rate
// while the machine still decides the verdict. The date shim (T442) is the
// named mechanism; these tests are what keep it from rotting into a prose
// comment.

const t450LoadSpinners = 32 // same shape the T437 reproducer used

// TestT450ThrashOracleStaysGreenUnderLoad is the standing load ratchet.
// The subject runs in a subprocess so the spinners contend for CPU the way
// a loaded full `go test ./...` does, without sharing the subject's process
// table (fake daemons, ports, locks).
func TestT450ThrashOracleStaysGreenUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("load ratchet is not a -short test")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		t.Skip("lsof unavailable; thrash oracle needs it")
	}

	stop := t450Load(t, t450LoadSpinners)
	defer stop()

	root := repoRoot(t)
	const repeats = 2
	cmd := exec.Command("go", "test", "-json", "-count", strconv.Itoa(repeats),
		"-timeout", "8m",
		"-run", "^TestRestartThrashPolicy$",
		"./scripts/docratchet")
	cmd.Dir = root
	// Fleet agents inherit JEVONS_RESTART_{DETACHED,LOCKED}=1 from the daily
	// daemon (T442). Scrub at this boundary so the subject oracle takes the
	// lock even when the in-tree thrashEnv scrub is not yet on HEAD.
	cmd.Env = t450ScrubRestartEnv(os.Environ())
	out, err := cmd.CombinedOutput()

	ran, skipped, failed := t450Verdicts(string(out))
	name := "TestRestartThrashPolicy"
	switch {
	case skipped[name] > 0:
		t.Errorf("%s was SKIPPED under load (%d of %d). 🎯T450 is not satisfied by declining to run the oracle that used to go red under load.",
			name, skipped[name], repeats)
	case failed[name] > 0:
		t.Errorf("%s failed %d of %d runs under %d busy loops — the load-sensitive flake 🎯T450 closes.\n%s",
			name, failed[name], repeats, t450LoadSpinners, t450Tail(string(out)))
	case ran[name] != repeats:
		t.Errorf("%s observed passing %d times, want %d.\n%s",
			name, ran[name], repeats, t450Tail(string(out)))
	}
	if err != nil && !t.Failed() {
		t.Errorf("go test under load failed for another reason: %v\n%s", err, t450Tail(string(out)))
	}
}

// TestT450SkipWaitMutationFails asserts the wait line + one-sided sleep
// bound still bite a script that skips the thrash wait. Without this, a
// future edit could delete `sleep "$remain"` and keep the suite green by
// never waiting — which is exactly the 🎯T194 failure the wait exists to
// prevent (claim success while the old binary can still be serving inside
// the window, or more precisely: skip the rate-limit without evidence).
func TestT450SkipWaitMutationFails(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		t.Skip("lsof unavailable")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go unavailable")
	}

	e := newThrashEnv(t)
	// Early-return the whole await: deleting only `sleep` leaves the wait
	// line intact and can still take ≥4s on a slow restart, so the elapsed
	// bound would not reliably falsify. Neutering the function bites the
	// exact-arithmetic string the oracle matches.
	mutateScript(t, e.script,
		"await_min_interval() {\n  # A real binary change inside the thrash window waits out the remainder",
		"await_min_interval() {\n  return 0 # 🎯T450 mutant: skip entire thrash wait\n  # A real binary change inside the thrash window waits out the remainder",
	)

	e.build("a")
	out, err := e.run(0)
	if err != nil {
		t.Fatalf("cold start failed: %v\n%s", err, out)
	}
	e.build("b")
	e.setClock(fixedClockEpoch + thrashElapsedSec)
	out, err = e.run(thrashWindowSec)
	if err != nil {
		t.Fatalf("mutant changed-build run failed: %v\n%s", err, out)
	}

	wantWait := fmt.Sprintf("thrash window: last restart %ds ago (min %ds); waiting %ds",
		thrashElapsedSec, thrashWindowSec, thrashRemainSec)
	if strings.Contains(out, wantWait) {
		t.Fatalf("skip-wait mutant still printed %q — the wait-line assertion would not have bitten.\nout:\n%s", wantWait, out)
	}
	t.Logf("skip-wait mutant bitten: wantWait %q absent", wantWait)
}

// TestT450StaleBinaryMutationFails asserts the variant-served check still
// bites a script that claims OK without activating the new binary.
func TestT450StaleBinaryMutationFails(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		t.Skip("lsof unavailable")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go unavailable")
	}

	e := newThrashEnv(t)
	e.build("a")
	out, err := e.run(0)
	if err != nil {
		t.Fatalf("cold start failed: %v\n%s", err, out)
	}
	if got := e.variantServed(); got != "a" {
		t.Fatalf("cold start variant %q", got)
	}

	// Mutate only after a real daemon is serving: the cold start must use
	// the honest script, otherwise there is no "stale" binary to leave up.
	body, err := os.ReadFile(e.script)
	if err != nil {
		t.Fatal(err)
	}
	mutated := string(body)
	for _, pair := range [][2]string{
		{`kill_port_listeners`, `true # 🎯T450 mutant: skip kill_port_listeners`},
		{`start_daemon_detached`, `true # 🎯T450 mutant: skip start_daemon_detached`},
		{`wait_until_serving`, `true # 🎯T450 mutant: skip wait_until_serving`},
	} {
		// Bare call sites in main only — not the function definitions.
		oldCall := "\n" + pair[0] + "\n"
		newCall := "\n" + pair[1] + "\n"
		if !strings.Contains(mutated, oldCall) {
			t.Fatalf("cannot locate call site %q to mutate", pair[0])
		}
		mutated = strings.Replace(mutated, oldCall, newCall, 1)
	}
	mutated = strings.Replace(mutated,
		`log "OK: daily jevonsd serving on :$PORT (workdir=$WORKDIR)"`,
		`log "OK: daily jevonsd serving on :$PORT (workdir=$WORKDIR) # 🎯T450 mutant lie"`,
		1)
	if err := os.WriteFile(e.script, []byte(mutated), 0o755); err != nil {
		t.Fatal(err)
	}

	e.build("b")
	e.setClock(fixedClockEpoch + thrashElapsedSec)
	out, err = e.run(thrashWindowSec)
	got := e.variantServed()
	if got == "b" {
		t.Fatalf("stale-binary mutant unexpectedly activated variant b — mutation did not take.\nout:\n%s", out)
	}
	if got != "a" {
		t.Fatalf("after mutant 'activation', :%d serves %q, want stale %q\nout:\n%s", e.port, got, "a", out)
	}
	// This is the oracle assertion that must fail on the mutant tree:
	//   if got := e.variantServed(); got != "b" { t.Fatalf(...) }
	t.Logf("stale-binary mutant bitten: still serving %q after claimed OK (run err=%v)", got, err)
}

// TestT450SiblingDaemonWaitTestsUnderLoad checks the other docratchet tests
// that touch the restart script / daemon path under the same load. Pure
// marker ratchets (t191, t405 docs) must stay green; if a sibling grows a
// wall-clock thrash window of its own, this is where it shows up.
func TestT450SiblingDaemonWaitTestsUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("load ratchet is not a -short test")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}

	stop := t450Load(t, t450LoadSpinners)
	defer stop()

	// Exact names: prose/script ratchets that live next to the thrash oracle.
	// TestRestartThrashPolicy itself is covered by TestT450ThrashOracleStaysGreenUnderLoad.
	filter := "^(TestRestartDailyJevonsdScript|TestRestartThrashPolicyDocumented|TestT405SelfDetachIsUnconditional|TestT405SupervisorIsBuiltAndInstallable|TestT405OracleIsCommitted)$"
	cmd := exec.Command("go", "test", "-json", "-count", "1",
		"-timeout", "2m",
		"-run", filter,
		"./scripts/docratchet")
	cmd.Dir = repoRoot(t)
	cmd.Env = t450ScrubRestartEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	ran, skipped, failed := t450Verdicts(string(out))
	want := []string{
		"TestRestartDailyJevonsdScript",
		"TestRestartThrashPolicyDocumented",
		"TestT405SelfDetachIsUnconditional",
		"TestT405SupervisorIsBuiltAndInstallable",
		"TestT405OracleIsCommitted",
	}
	for _, name := range want {
		switch {
		case skipped[name] > 0:
			t.Errorf("%s skipped under load", name)
		case failed[name] > 0:
			t.Errorf("%s failed under load:\n%s", name, t450Tail(string(out)))
		case ran[name] == 0:
			t.Errorf("%s did not run under load", name)
		}
	}
	if err != nil && !t.Failed() {
		t.Errorf("sibling package run failed: %v\n%s", err, t450Tail(string(out)))
	}
}

func mutateScript(t *testing.T, script, old, new string) {
	t.Helper()
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, old) {
		t.Fatalf("script missing mutate target %q", old)
	}
	s = strings.Replace(s, old, new, 1)
	if err := os.WriteFile(script, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}
}

// t450ScrubRestartEnv drops the restart script's re-exec flags. Same defect
// class as T442's thrashEnv scrub: a fleet-agent process tree inherits
// DETACHED/LOCKED=1 from the daily daemon and would otherwise skip the lock.
func t450ScrubRestartEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "JEVONS_RESTART_LOCKED",
			"JEVONS_RESTART_DETACHED",
			"JEVONS_RESTART_NO_LOCK",
			"JEVONS_RESTART_NO_DETACH",
			"JEVONS_RESTART_FAULT":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// t450Load saturates with n arithmetic spinners. Same disease as t400Load;
// kept local so T450's spinner count (32, the T437 reproducer) is explicit
// and does not drift with T400's NumCPU*3 policy.
func t450Load(t *testing.T, n int) func() {
	t.Helper()
	if n < 1 {
		n = runtime.NumCPU()
	}
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
		t.Logf("🎯T450 load: %d spinners, %d iterations over %s", n, got, time.Since(start).Round(time.Millisecond))
		if got == 0 {
			t.Error("load generator never completed a spin — ratchet ran idle")
		}
	}
}

func t450Verdicts(stream string) (pass, skip, fail map[string]int) {
	pass, skip, fail = map[string]int{}, map[string]int{}, map[string]int{}
	for _, line := range strings.Split(stream, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
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

func t450Tail(stream string) string {
	lines := strings.Split(strings.TrimSpace(stream), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return strings.Join(lines, "\n")
}

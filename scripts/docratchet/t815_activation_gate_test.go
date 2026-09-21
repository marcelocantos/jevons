// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 🎯T815 — the activation gate. Executable oracle: N activation requests
// inside one interval produce one bounce and N answers naming the same
// served SHA. It runs the committed restart script (the one workers run by
// hand) against a fake daemon on a throwaway port, with the injected clock
// from 🎯T442 and a stub-free real runlock/detach.

// git815 runs git in dir and returns trimmed stdout.
func git815(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (e *thrashEnv) commit(t *testing.T, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.root, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git815(t, e.root, "add", name)
	git815(t, e.root, "commit", "-m", name)
	return git815(t, e.root, "rev-parse", "HEAD")
}

func (e *thrashEnv) servedLines(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.home, ".jevons", "restart-daily.served"))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestActivationGateCoalescesRequests(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		t.Skip("lsof unavailable")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable for the fake daemon")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}

	e := newThrashEnv(t)
	git815(t, e.root, "init", "-q")
	e.commit(t, "c1")
	e.build("a")

	// Cold start: nothing serving, so the gate must not defer it.
	out, err := e.run(thrashWindowSec)
	if err != nil {
		t.Fatalf("cold start: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK: development jevonsd serving") {
		t.Fatalf("cold start did not activate:\n%s", out)
	}
	first := e.listenerPID()
	baseline := len(e.servedLines(t))

	// Four workers land commits and each asks for an activation inside the
	// interval.
	e.build("b")
	head := e.commit(t, "c2")
	e.setClock(fixedClockEpoch + thrashElapsedSec)

	const callers = 4
	var wg sync.WaitGroup
	outs := make([]string, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outs[i], errs[i] = e.run(thrashWindowSec)
		}(i)
	}
	wg.Wait()
	for i, o := range outs {
		if errs[i] != nil {
			t.Fatalf("request %d failed: %v\n%s", i, errs[i], o)
		}
		if !strings.Contains(o, "activation deferred") {
			t.Errorf("request %d was not deferred:\n%s", i, o)
		}
		if strings.Contains(o, "OK: development jevonsd serving") {
			t.Errorf("request %d bounced inside the interval:\n%s", i, o)
		}
	}
	if got := e.listenerPID(); got != first {
		t.Fatalf("daemon pid %d → %d: a request inside the interval bounced", first, got)
	}
	if got := e.variantServed(); got != "a" {
		t.Fatalf("variant %q served inside the interval, want a", got)
	}

	// The deferred bounce happens once, when the interval ends.
	deadline := time.Now().Add(90 * time.Second)
	for len(e.servedLines(t)) <= baseline && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
	}
	lines := e.servedLines(t)
	if len(lines) != baseline+1 {
		t.Fatalf("served ledger has %d lines, want %d (one deferred bounce): %v", len(lines), baseline+1, lines)
	}
	got := lines[len(lines)-1]
	if !strings.Contains(got, " "+head+" ") && !strings.HasSuffix(strings.Fields(got)[2], head) {
		t.Errorf("served line %q does not name HEAD %s", got, head)
	}
	if !strings.Contains(got, "coalesced=4") {
		t.Errorf("served line %q does not show the 4 coalesced requests", got)
	}
	if v := e.variantServed(); v != "b" {
		t.Errorf("after the deferred bounce :%d serves %q, want b", e.port, v)
	}
	second := e.listenerPID()
	if second == first || second == 0 {
		t.Errorf("no bounce happened: pid %d → %d", first, second)
	}

	// Each requester can ask which bounce carried its commit.
	q, err := e.run(thrashWindowSec, "--served", head)
	if err != nil {
		t.Fatalf("--served %s: %v\n%s", head, err, q)
	}
	if !strings.Contains(q, head) {
		t.Errorf("--served answer does not name the served SHA:\n%s", q)
	}
}

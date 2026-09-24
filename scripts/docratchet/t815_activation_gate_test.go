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
	b, err := os.ReadFile(filepath.Join(e.home, ".jevons", "restart-jevonsd.served"))
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

// ownerToken815 writes the owner's --force token (🎯T815 owner-only bypass).
func (e *thrashEnv) ownerToken(t *testing.T, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(e.home, ".jevons", "restart-jevonsd.owner-force")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // WriteFile honours the umask
		t.Fatal(err)
	}
	return p
}

func (e *thrashEnv) waitServedLines(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for len(e.servedLines(t)) < n && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
	}
	if got := len(e.servedLines(t)); got != n {
		t.Fatalf("served ledger has %d lines, want %d: %v", got, n, e.servedLines(t))
	}
}

// 🎯T815 owner-only --force: a plain --force is an ordinary request; only the
// owner's token bypasses the gate, and it is single-use.
func TestActivationGateForceIsOwnerOnly(t *testing.T) {
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
	if out, err := e.run(thrashWindowSec); err != nil || !strings.Contains(out, "OK: development jevonsd serving") {
		t.Fatalf("cold start: %v\n%s", err, out)
	}
	first := e.listenerPID()
	baseline := len(e.servedLines(t))

	// A worker's plain --force inside the interval: deferred, not bounced.
	e.build("b")
	e.commit(t, "c2")
	e.setClock(fixedClockEpoch + thrashElapsedSec)
	out, err := e.run(thrashWindowSec, "--force")
	if err != nil {
		t.Fatalf("plain --force: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OWNER-ONLY") || !strings.Contains(out, "jevons-po") {
		t.Errorf("refusal does not name the owner-only rule and jevons-po:\n%s", out)
	}
	if !strings.Contains(out, "activation deferred") || strings.Contains(out, "OK: development jevonsd serving") {
		t.Errorf("plain --force bypassed the gate:\n%s", out)
	}
	if got := e.listenerPID(); got != first {
		t.Fatalf("daemon pid %d → %d: a plain --force bounced inside the interval", first, got)
	}
	// Wrong-shaped tokens are not the owner's: group-readable, and stale.
	tok := e.ownerToken(t, 0o644)
	out, _ = e.run(thrashWindowSec, "--force")
	if !strings.Contains(out, "OWNER-ONLY") || strings.Contains(out, "OK: development jevonsd serving") {
		t.Errorf("mode-0644 token was honoured:\n%s", out)
	}
	if err := os.Chmod(tok, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(fixedClockEpoch-100000, 0)
	if err := os.Chtimes(tok, old, old); err != nil {
		t.Fatal(err)
	}
	out, _ = e.run(thrashWindowSec, "--force")
	if !strings.Contains(out, "OWNER-ONLY") || strings.Contains(out, "OK: development jevonsd serving") {
		t.Errorf("stale token was honoured:\n%s", out)
	}
	os.Remove(tok)
	if got := e.listenerPID(); got != first {
		t.Fatalf("daemon pid %d → %d: a refused --force bounced", first, got)
	}
	// The deferred runner still delivers the one ordinary bounce.
	e.waitServedLines(t, baseline+1)
	afterDeferred := e.listenerPID()

	// Control: the owner's token bypasses the gate at once, and is consumed.
	e.build("c")
	e.commit(t, "c3")
	e.setClock(fixedClockEpoch + 2*thrashElapsedSec)
	tok = e.ownerToken(t, 0o600)
	out, err = e.run(thrashWindowSec, "--force")
	if err != nil {
		t.Fatalf("owner --force: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK: development jevonsd serving") || strings.Contains(out, "activation deferred") {
		t.Fatalf("owner --force did not bypass the gate:\n%s", out)
	}
	if v := e.variantServed(); v != "c" {
		t.Errorf("owner --force: :%d serves %q, want c", e.port, v)
	}
	if got := e.listenerPID(); got == afterDeferred || got == 0 {
		t.Errorf("owner --force did not bounce: pid %d → %d", afterDeferred, got)
	}
	if _, err := os.Stat(tok); err == nil {
		t.Errorf("owner token survived its use")
	}
	// Single use: the next --force is a plain one again.
	e.build("d")
	e.commit(t, "c4")
	out, _ = e.run(thrashWindowSec, "--force")
	if !strings.Contains(out, "OWNER-ONLY") || strings.Contains(out, "OK: development jevonsd serving") {
		t.Errorf("a consumed token was honoured twice:\n%s", out)
	}
}

// 🎯T815: the other bypasses are untouched — a dead daemon is never deferred,
// with or without --force (the watchdog passes none).
func TestActivationGateDaemonDownStillBypasses(t *testing.T) {
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
	if out, err := e.run(thrashWindowSec); err != nil || !strings.Contains(out, "OK: development jevonsd serving") {
		t.Fatalf("cold start: %v\n%s", err, out)
	}
	e.setClock(fixedClockEpoch + thrashElapsedSec) // inside the interval

	for _, args := range [][]string{{"--force"}, {}} {
		e.killDaemon()
		out, err := e.run(thrashWindowSec, args...)
		if err != nil {
			t.Fatalf("daemon-down restart %v: %v\n%s", args, err, out)
		}
		if strings.Contains(out, "activation deferred") || !strings.Contains(out, "OK: development jevonsd serving") {
			t.Errorf("daemon-down restart %v was deferred or did not serve:\n%s", args, out)
		}
		if strings.Contains(out, "waiting") && strings.Contains(out, "thrash window") {
			t.Errorf("daemon-down restart %v waited out the thrash window:\n%s", args, out)
		}
	}
}

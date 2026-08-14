// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 🎯T218 — executable oracle for the restart thrash policy.
//
// This is not a prose ratchet: it runs the committed restart script against
// a fake daemon on a throwaway port and asserts what the daily port actually
// experiences. The motivating incident (~/.jevons/restart-daily.log,
// 2026-08-05T19:15–19:19) was five restarts in four minutes, every one of
// them rebuilding nothing — a healthy daemon SIGTERMed and replaced by the
// byte-identical binary, leaving owner chat unusable.
//
// The three properties under test, in the order they matter:
//
//	no-op   identical build → the serving pid survives untouched
//	coalesce concurrent callers → exactly one bounce, not N
//	no-skip  changed build → always activated, even inside the window (🎯T194)
//
// The last one is why the policy waits instead of skipping: a skip would
// report success while a stale binary kept serving.

// fakeDaemonSrc stands in for jevonsd: it takes the same flags and answers
// the two endpoints the script probes. Built twice with different -ldflags
// to simulate "the binary changed".
const fakeDaemonSrc = `package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

var variant = "a"

func main() {
	port := flag.Int("port", 0, "")
	workdir := flag.String("workdir", "", "")
	flag.Parse()
	_ = *workdir
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "{\"status\":\"ok\",\"variant\":%q}", variant)
	})
	http.HandleFunc("/api/frontier", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "{\"targets\":[]}")
	})
	if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *port), nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

// 🎯T442 — the thrash window is injected, never inferred from real time.
//
// The window is a comparison between two clock readings: the epoch the script
// stamped after its last successful restart, and `date +%s` when the next
// caller asks. Read from the real clock, the gap is whatever the machine took
// to get from one to the other — four no-op script runs and a `go build`, here
// — so "inside a 6s window" was a statement about this laptop's load, and the
// ratchet went red once at HEAD (gate cde5b3e4) with the restart log showing a
// perfectly healthy bounce. A longer window would only lower the firing rate;
// the reading has to stop being a race.
//
// So the harness owns the clock: a `date` shim first on the script's PATH
// answers `+%s` from a file the test writes, and delegates every other format
// (the log timestamps) to the real thing. Elapsed is then exactly what the
// test set, however slow the machine is, and the assertions below can name the
// arithmetic — "2s ago (min 6s); waiting 4s" — instead of hoping for a
// substring. Remove the shim and those numbers become real-clock noise, so the
// exact match is also what guards the injection.
const (
	fixedClockEpoch  = 1700000000 // frozen "now"; no real time enters the window
	thrashWindowSec  = 6          // MIN_INTERVAL_SEC for the changed-build run
	thrashElapsedSec = 2          // injected age of the last restart stamp
	thrashRemainSec  = thrashWindowSec - thrashElapsedSec
)

// dateShim answers `date +%s` from clockFile and delegates everything else to
// the real date. Written per-env so nothing outside this test sees it.
const dateShim = `#!/bin/bash
# 🎯T442 fixed-clock shim — see t218_restart_thrash_test.go.
if [ "$#" -eq 1 ] && [ "$1" = "+%%s" ]; then
  cat %q
  exit 0
fi
exec %q "$@"
`

// thrashEnv is one hermetic universe: a copied script, a throwaway port, a
// private HOME, its own stamp/lock/active files, and its own clock.
type thrashEnv struct {
	t         *testing.T
	root      string // fake repo root (script's ROOT, so BIN = root/bin/jevonsd)
	home      string
	port      int
	script    string
	srcDir    string
	shimDir   string // 🎯T442: holds the `date` shim, first on the script's PATH
	clockFile string // 🎯T442: the epoch that shim answers `date +%s` with
}

func newThrashEnv(t *testing.T) *thrashEnv {
	t.Helper()

	// Universe B discipline: never the daily port, whatever the OS hands us.
	port := freeTCPPort(t)
	if port == 13705 {
		t.Fatalf("refusing daily port %d", port)
	}

	root := t.TempDir()
	home := t.TempDir()
	srcDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts/restart-daily-jevonsd.sh"))
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	script := filepath.Join(root, "scripts", "restart-daily-jevonsd.sh")
	if err := os.WriteFile(script, body, 0o755); err != nil {
		t.Fatal(err)
	}

	// 🎯T392.5 (runlock) and 🎯T405 (detach): the script re-execs itself
	// through both and refuses to run when either binary is missing. The
	// script's own env has a bare PATH with no `go`, so build the REAL
	// binaries here, with the test's environment, rather than substituting
	// stubs — a stub runlock that merely execs its command would let every
	// concurrent caller through and quietly turn the coalescing assertions
	// below into no-ops, and a stub detach would drop the property that the
	// bounce outlives its caller.
	for _, helper := range []string{"runlock", "detach"} {
		build := exec.Command("go", "build", "-o",
			filepath.Join(root, "bin", helper),
			filepath.Join(repoRoot(t), "cmd", helper))
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s for the harness: %v\n%s", helper, err, out)
		}
	}

	// Fake daemon module, rebuilt per variant.
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(fakeDaemonSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module fakejevonsd\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 🎯T442: the injected clock. realDate is resolved here, from the test's
	// own PATH, because the shim's PATH deliberately does not contain itself.
	realDate, err := exec.LookPath("date")
	if err != nil {
		t.Skipf("no date binary to delegate to: %v", err)
	}
	shimDir := filepath.Join(root, "clockshim")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	clockFile := filepath.Join(home, "clock.epoch")
	shim := fmt.Sprintf(dateShim, clockFile, realDate)
	if err := os.WriteFile(filepath.Join(shimDir, "date"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}

	e := &thrashEnv{
		t: t, root: root, home: home, port: port,
		script: script, srcDir: srcDir,
		shimDir: shimDir, clockFile: clockFile,
	}
	e.setClock(fixedClockEpoch)
	t.Cleanup(e.killDaemon)
	return e
}

// build writes bin/jevonsd for the given variant. Different variant ⇒
// different bytes ⇒ different sha ⇒ "the binary changed".
func (e *thrashEnv) build(variant string) {
	e.t.Helper()
	out := filepath.Join(e.root, "bin", "jevonsd")
	cmd := exec.Command("go", "build",
		"-ldflags", "-X main.variant="+variant,
		"-o", out, ".")
	cmd.Dir = e.srcDir
	if b, err := cmd.CombinedOutput(); err != nil {
		e.t.Fatalf("build fake daemon %q: %v\n%s", variant, err, b)
	}
}

// setClock moves the injected clock. Every `date +%s` the script runs after
// this — the stamp it writes, and the elapsed it computes next time — reads
// this value, so the thrash window is arithmetic the test chose.
func (e *thrashEnv) setClock(epoch int) {
	e.t.Helper()
	if err := os.WriteFile(e.clockFile, []byte(strconv.Itoa(epoch)+"\n"), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// run invokes the script and returns its combined output.
func (e *thrashEnv) run(minInterval int, args ...string) (string, error) {
	e.t.Helper()
	cmd := exec.Command("/bin/bash", append([]string{e.script}, args...)...)
	cmd.Dir = e.root
	// A bare PATH keeps `brew` out of reach: the real script would otherwise
	// run `brew services stop jevons` and stop the owner's daily service.
	cmd.Env = append(os.Environ(),
		// 🎯T442: the shim dir goes first so `date +%s` is the injected clock.
		"PATH="+e.shimDir+":/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME="+e.home,
		"JEVONS_RESTART_PORT="+strconv.Itoa(e.port),
		"JEVONS_RESTART_WORKDIR="+e.root,
		"JEVONS_RESTART_LOG="+filepath.Join(e.home, "daemon.log"),
		"JEVONS_RESTART_WAIT_SEC=30",
		"JEVONS_RESTART_SKIP_MAKE=1",
		"JEVONS_RESTART_MIN_INTERVAL_SEC="+strconv.Itoa(minInterval),
		"JEVONS_RESTART_LOCK_WAIT_SEC=90",
		"JEVONS_RESTART_STAMP="+filepath.Join(e.home, "restart.last"),
		"JEVONS_RESTART_ACTIVE="+filepath.Join(e.home, "restart.active"),
		"JEVONS_RESTART_LOCK="+filepath.Join(e.home, "restart.lock"),
		// 🎯T405: one detach log per caller. The detached child writes to
		// this file and the parent streams it back to us, so callers sharing
		// one log read each other's lines — and the assertions below, which
		// ask what THIS caller did, would count a neighbour's "OK: serving"
		// as their own.
		"JEVONS_RESTART_DETACH_LOG="+filepath.Join(e.home, "detach", nextDetachLog()),
	)
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// detachLogSeq names each run's detach log; the concurrency case below
// runs several at once, so the counter is atomic.
var detachLogSeq atomic.Int64

func nextDetachLog() string {
	return fmt.Sprintf("run-%d.log", detachLogSeq.Add(1))
}

// listenerPID is the pid currently holding the test port, or 0.
func (e *thrashEnv) listenerPID() int {
	e.t.Helper()
	out, err := exec.Command("/usr/sbin/lsof", "-nP",
		"-iTCP:"+strconv.Itoa(e.port), "-sTCP:LISTEN", "-t").Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return pid
}

// variantServed reports which build is actually answering /health — the
// product-truth check that a restart really activated the new binary.
func (e *thrashEnv) variantServed() string {
	e.t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", e.port))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	// {"status":"ok","variant":"b"}
	_, rest, ok := strings.Cut(body, `"variant":"`)
	if !ok {
		return ""
	}
	v, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return ""
	}
	return v
}

func (e *thrashEnv) killDaemon() {
	for range 10 {
		pid := e.listenerPID()
		if pid == 0 {
			return
		}
		p, err := os.FindProcess(pid)
		if err != nil {
			return
		}
		_ = p.Kill()
		time.Sleep(100 * time.Millisecond)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestRestartThrashPolicy is the 🎯T218 oracle: identical builds never bounce
// the port, concurrent callers coalesce into one bounce, and a changed build
// is always activated rather than skipped.
func TestRestartThrashPolicy(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		t.Skip("lsof unavailable; policy oracle needs it to read the listening pid")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable for the fake daemon")
	}

	e := newThrashEnv(t)
	e.build("a")

	// --- cold start: nothing serving, so a restart must happen -------------
	out, err := e.run(0)
	if err != nil {
		t.Fatalf("cold start failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK: daily jevonsd serving") {
		t.Fatalf("cold start did not reach serving state:\n%s", out)
	}
	first := e.listenerPID()
	if first == 0 {
		t.Fatalf("nothing listening on :%d after cold start:\n%s", e.port, out)
	}
	if got := e.variantServed(); got != "a" {
		t.Fatalf("serving variant %q, want %q", got, "a")
	}

	// --- THE THRASH ORACLE: identical build must not touch the port -------
	// This is the 2026-08-05 incident in miniature. Before the policy, this
	// call killed `first` and started an identical replacement.
	out, err = e.run(0)
	if err != nil {
		t.Fatalf("no-op run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "already activated") {
		t.Errorf("identical build did not short-circuit:\n%s", out)
	}
	if strings.Contains(out, "killing listeners") {
		t.Errorf("identical build killed the daemon (thrash):\n%s", out)
	}
	if got := e.listenerPID(); got != first {
		t.Errorf("serving pid changed %d → %d on an unchanged build; the daemon was bounced for nothing", first, got)
	}

	// Repeat: thrash is a *sustained* failure, so one no-op is not enough.
	for i := range 3 {
		if out, err = e.run(0); err != nil {
			t.Fatalf("no-op run %d failed: %v\n%s", i, err, out)
		}
	}
	if got := e.listenerPID(); got != first {
		t.Errorf("serving pid changed %d → %d across repeated unchanged calls", first, got)
	}

	// --- changed build inside the window: wait, never skip (🎯T194) -------
	e.build("b")
	// 🎯T442: put the caller inside the window by moving the clock, not by
	// racing to arrive before it closes. The cold start stamped
	// fixedClockEpoch; thrashElapsedSec later is inside a thrashWindowSec
	// window on any machine, at any load, however long the builds above took.
	e.setClock(fixedClockEpoch + thrashElapsedSec)
	start := time.Now()
	out, err = e.run(thrashWindowSec)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("changed-build run failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "already activated") {
		t.Fatalf("changed build was treated as already activated:\n%s", out)
	}
	// The exact arithmetic, not a substring: these three numbers are only
	// reproducible because the clock is injected, so matching them is what
	// keeps a future edit from quietly going back to real time.
	wantWait := fmt.Sprintf("thrash window: last restart %ds ago (min %ds); waiting %ds",
		thrashElapsedSec, thrashWindowSec, thrashRemainSec)
	if !strings.Contains(out, wantWait) {
		t.Errorf("changed build inside the window did not report waiting %q:\n%s", wantWait, out)
	}
	if !strings.Contains(out, "OK: daily jevonsd serving") {
		t.Fatalf("changed build was skipped instead of activated — a stale binary would keep serving:\n%s", out)
	}
	// A real `sleep thrashRemainSec` cannot come back early, so this bound is
	// one-sided and load-proof: waiting longer never fails it.
	if elapsed < thrashRemainSec*time.Second {
		t.Errorf("restart took %v; expected to wait out the %ds remaining of the thrash window",
			elapsed, thrashRemainSec)
	}
	second := e.listenerPID()
	if second == first || second == 0 {
		t.Errorf("changed build did not replace the daemon (pid %d → %d)", first, second)
	}
	if got := e.variantServed(); got != "b" {
		t.Fatalf("after activating the new build, :%d still serves variant %q — this is the stale-binary failure 🎯T194 forbids", e.port, got)
	}

	// --- concurrency: N callers, one bounce -------------------------------
	e.build("c")
	const callers = 4
	var wg sync.WaitGroup
	outs := make([]string, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outs[i], errs[i] = e.run(0)
		}(i)
	}
	wg.Wait()

	restarts, noops := 0, 0
	for i, o := range outs {
		if errs[i] != nil {
			t.Fatalf("concurrent caller %d failed: %v\n%s", i, errs[i], o)
		}
		if strings.Contains(o, "OK: daily jevonsd serving") {
			restarts++
		}
		if strings.Contains(o, "already activated") || strings.Contains(o, "coalesced") {
			noops++
		}
	}
	if restarts != 1 {
		t.Errorf("%d of %d concurrent callers bounced the port; want exactly 1 (the rest must coalesce)", restarts, callers)
	}
	if noops != callers-1 {
		t.Errorf("%d callers coalesced; want %d", noops, callers-1)
	}
	if got := e.variantServed(); got != "c" {
		t.Errorf("after the coalesced restart, :%d serves variant %q, want %q", e.port, got, "c")
	}

	// --- a dead lock holder must not wedge the fleet forever --------------
	//
	// 🎯T392.5 changed how this is guaranteed. The old mkdir mutex had to
	// detect a corpse and break the lock by hand, and the window between
	// creating the directory and writing the pid file was itself the bug:
	// a second caller read an empty pid, called a live holder stale, and
	// deleted the lock out from under it. flock(2) has no such window —
	// the kernel drops the lock when the holder dies, however it dies — so
	// the test is now that a leftover lock *file* is not a held lock.
	lock := filepath.Join(e.home, "restart.lock")
	if err := os.WriteFile(lock, []byte(strconv.Itoa(deadPID(t))+" corpse\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.build("d")
	out, err = e.run(0)
	if err != nil {
		t.Fatalf("run behind a dead holder's leftover lock file failed: %v\n%s", err, out)
	}
	if got := e.variantServed(); got != "d" {
		t.Errorf("a leftover lock file wedged the restart: :%d serves variant %q, want %q", e.port, got, "d")
	}
}

// deadPID returns a pid that has certainly exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/usr/bin/true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn throwaway process: %v", err)
	}
	return cmd.Process.Pid
}

// TestRestartThrashPolicyDocumented keeps the policy's rationale in the
// script itself: the next reader must find out *why* it usually does
// nothing before they "fix" it by deleting the check.
func TestRestartThrashPolicyDocumented(t *testing.T) {
	body := readRepo(t, "scripts/restart-daily-jevonsd.sh")
	for _, m := range []string{
		"🎯T218",
		"ALREADY-ACTIVATED",
		"ONE-AT-A-TIME",
		"WAIT, DON'T SKIP",
		"🎯T194",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("restart script missing thrash-policy marker %q", m)
		}
	}
}

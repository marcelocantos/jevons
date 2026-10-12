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

	"github.com/marcelocantos/jevons/internal/testreap"
)

// 🎯T218 — executable oracle for the restart thrash policy.
//
// This is not a prose ratchet: it runs the committed restart script against
// a fake daemon on a throwaway port and asserts what the development port actually
// experiences. The motivating incident (~/.jevons/restart-jevonsd.log,
// 2026-08-05T19:15–19:19) was five restarts in four minutes, every one of
// them rebuilding nothing — a healthy daemon SIGTERMed and replaced by the
// byte-identical binary, leaving owner chat unusable.
//
// The three properties under test, in the order they matter:
//
//	no-op    identical build → the serving pid survives untouched
//	coalesce concurrent callers → exactly one bounce, not N
//	activate changed build → activated at once, never skipped (🎯T194)
//
// There is no rate limit (owner, 2026-10-12): a skip would report success
// while a stale binary kept serving, and a delay only held fixes back.

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
	// The script starts the daemon with -vanilla-port too; an unknown flag
	// is a parse error and the fixture would die before binding anything.
	vanillaPort := flag.Int("vanilla-port", 0, "")
	flag.Parse()
	_ = *workdir
	_ = *vanillaPort
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

// thrashEnv is one hermetic universe: a copied script, a throwaway port, a
// private HOME, and its own lock and active files.
type thrashEnv struct {
	t      *testing.T
	root   string // fake repo root (script's ROOT, so BIN = root/bin/jevonsd)
	home   string
	port   int
	script string
	srcDir string
}

func newThrashEnv(t *testing.T) *thrashEnv {
	t.Helper()

	// Universe B discipline: never the development port, whatever the OS hands us.
	port := freeTCPPort(t)
	if port == 13705 {
		t.Fatalf("refusing development port %d", port)
	}

	// 🎯T932: killDaemon below runs only when cleanups do. -timeout and a
	// SIGKILLed test binary run none, and the daemons are detached, so the
	// sweep must outlive the binary. Armed before anything is started.
	testreap.Arm(t)
	root := t.TempDir()
	home := t.TempDir()
	srcDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 🎯T540.2: the script refuses to bring :PORT up without a React bundle
	// to serve. The fixture daemon serves nothing of the sort, but the check
	// runs before it starts, so the fixture needs the file to exist at all.
	if err := os.MkdirAll(filepath.Join(root, "ui", "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ui", "dist", "index.html"), []byte("<!doctype html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 🎯T448: bin/claudiapin exits non-zero when go.mod names no claudia
	// pin at all, which would kill the fixture run before it starts.
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module fixture\n\ngo 1.26\n\nrequire github.com/marcelocantos/claudia v0.24.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts/restart-jevonsd.sh"))
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	script := filepath.Join(root, "scripts", "restart-jevonsd.sh")
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
	// 🎯T448 claudiapin dies on a silent pin; 🎯T580 buildident answers the
	// source identity. Both run with the script's bare PATH, which has no
	// `go` to build them on the fly, so the fixture supplies them.
	for _, helper := range []string{"runlock", "detach", "claudiapin", "buildident"} {
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

	e := &thrashEnv{
		t: t, root: root, home: home, port: port,
		script: script, srcDir: srcDir,
	}
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
	// 🎯T580: t.TempDir() hands out siblings under one parent, and that is
	// where the workspace fixture writes its go.work — which would otherwise
	// govern this build and fail it for a module it never heard of.
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		e.t.Fatalf("build fake daemon %q: %v\n%s", variant, err, b)
	}
}

// run invokes the script and returns its combined output.
func (e *thrashEnv) run(args ...string) (string, error) {
	e.t.Helper()
	cmd := exec.Command("/bin/bash", append([]string{e.script}, args...)...)
	cmd.Dir = e.root
	// A bare PATH keeps `brew` out of reach: the real script would otherwise
	// run `brew services stop jevons` and stop the owner's daily service.
	//
	// 🎯T442: strip the restart-script control vars out of os.Environ before
	// appending ours. The daily restart exports JEVONS_RESTART_DETACHED=1 and
	// JEVONS_RESTART_LOCKED=1 into the daemon it starts; fleet agents (and
	// therefore this test when run from one) inherit them. Left in place, the
	// script believes it is already the locked detached child and skips both
	// re-execs — four concurrent callers then race the port, which is exactly
	// the thrash the oracle exists to catch, attributed to the machine instead
	// of the tree. Filtering (not overwriting with "0") is required: a
	// duplicate key in the environ slice is OS-dependent, and "set to 0" would
	// still lose if the leaked "1" came later.
	cmd.Env = append(scrubRestartControlEnv(os.Environ()),
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME="+e.home,
		"JEVONS_RESTART_PORT="+strconv.Itoa(e.port),
		"JEVONS_RESTART_WORKDIR="+e.root,
		"JEVONS_RESTART_LOG="+filepath.Join(e.home, "daemon.log"),
		"JEVONS_RESTART_WAIT_SEC=30",
		"JEVONS_RESTART_STOP_WAIT_SEC=5",
		"JEVONS_RESTART_SKIP_MAKE=1",
		"JEVONS_RESTART_LOCK_WAIT_SEC=90",
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

// scrubRestartControlEnv drops the script's own re-exec / seam flags and
// lock-path knobs so a caller whose process tree inherited them (daily
// daemon started under the restart script) cannot make the oracle skip
// detach/lock or share a fleet lock file.
//
// 🎯T529: JEVONS_RESTART_LOCK / LOCK_WAIT_SEC must be filtered too — appending
// a fixture value after an inherited key is OS-dependent, so a fleet lock
// path left in the slice can still win and make the thrash verdict ambient.
func scrubRestartControlEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "JEVONS_RESTART_LOCKED",
			"JEVONS_RESTART_LOCK",
			"JEVONS_RESTART_LOCK_WAIT_SEC",
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

// killDaemon removes every daemon this env started, not merely the one
// holding the port. The script detaches its daemons (cmd/detach) so a bounce
// outlives its caller, and each bounce hands the port to a successor. A
// daemon that lost the race, or died before binding, is therefore invisible
// to lsof — and the old port-only cleanup returned on the first `pid == 0`,
// stranding it for the machine's remaining uptime. Seven such daemons were
// found alive on the owner's Mac, two of them four days old.
//
// Matching on the env's own temp root is what makes the sweep total while
// keeping the owner's real jevonsd, which lives outside it, unreachable.
func (e *thrashEnv) killDaemon() {
	testreap.ReapUnder(e.t, e.root)
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
	out, err := e.run()
	if err != nil {
		t.Fatalf("cold start failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK: development jevonsd serving") {
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
	out, err = e.run()
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
		if out, err = e.run(); err != nil {
			t.Fatalf("no-op run %d failed: %v\n%s", i, err, out)
		}
	}
	if got := e.listenerPID(); got != first {
		t.Errorf("serving pid changed %d → %d across repeated unchanged calls", first, got)
	}

	// --- changed build: activated at once, never skipped (🎯T194) ---------
	e.build("b")
	out, err = e.run()
	if err != nil {
		t.Fatalf("changed-build run failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "already activated") {
		t.Fatalf("changed build was treated as already activated:\n%s", out)
	}
	if !strings.Contains(out, "OK: development jevonsd serving") {
		t.Fatalf("changed build was not activated:\n%s", out)
	}
	second := e.listenerPID()
	if second == first || second == 0 {
		t.Errorf("changed build did not replace the daemon (pid %d → %d)", first, second)
	}
	if got := e.variantServed(); got != "b" {
		t.Fatalf(":%d still serves variant %q — this is the stale-binary failure 🎯T194 forbids", e.port, got)
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
			outs[i], errs[i] = e.run()
		}(i)
	}
	wg.Wait()

	restarts, noops := 0, 0
	for i, o := range outs {
		if errs[i] != nil {
			t.Fatalf("concurrent caller %d failed: %v\n%s", i, errs[i], o)
		}
		if strings.Contains(o, "OK: development jevonsd serving") {
			restarts++
		}
		if strings.Contains(o, "already activated") || strings.Contains(o, "coalesced") {
			noops++
		}
	}
	if restarts != 1 || noops != callers-1 {
		var b strings.Builder
		for i, o := range outs {
			fmt.Fprintf(&b, "\n--- caller %d ---\n%s", i, o)
		}
		t.Errorf("%d of %d concurrent callers bounced; %d coalesced; want 1 bounce and %d coalesced:%s",
			restarts, callers, noops, callers-1, b.String())
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
	out, err = e.run()
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
	body := readRepo(t, "scripts/restart-jevonsd.sh")
	for _, m := range []string{
		"🎯T218",
		"ALREADY-ACTIVATED",
		"ONE-AT-A-TIME",
		"🎯T194",
		// 🎯T442: the daemon must not inherit the re-exec flags, or every
		// agent it spawns skips the lock and races the port.
		"unset JEVONS_RESTART_DETACHED JEVONS_RESTART_LOCKED",
		// 🎯T529: lock-path knobs must not leak into the daemon either —
		// an inherited fleet LOCK path would share the fleet's lock file.
		"unset JEVONS_RESTART_LOCK JEVONS_RESTART_LOCK_WAIT_SEC",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("restart script missing thrash-policy marker %q", m)
		}
	}
}

// TestScrubRestartControlEnv is the hermetic half of the 🎯T442/T529 env-leak
// fix: a parent that exports the script's re-exec flags or a fleet
// JEVONS_RESTART_LOCK* path (the development daemon used to) must not be able to
// make the oracle skip detach/lock or share the fleet lock file.
func TestScrubRestartControlEnv(t *testing.T) {
	in := []string{
		"PATH=/bin",
		"JEVONS_RESTART_LOCKED=1",
		"JEVONS_RESTART_LOCK=/fleet/restart-jevonsd.lock",
		"JEVONS_RESTART_LOCK_WAIT_SEC=240",
		"JEVONS_RESTART_DETACHED=1",
		"JEVONS_RESTART_NO_LOCK=1",
		"JEVONS_RESTART_NO_DETACH=1",
		"JEVONS_RESTART_FAULT=after-kill",
		"JEVONS_RESTART_PORT=13705",
		"HOME=/tmp",
	}
	got := scrubRestartControlEnv(in)
	want := []string{"PATH=/bin", "JEVONS_RESTART_PORT=13705", "HOME=/tmp"}
	if len(got) != len(want) {
		t.Fatalf("scrubbed %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scrubbed[%d]=%q; want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

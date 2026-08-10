// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// The 🎯T405 oracle. Both tests here reproduce the 2026-08-10 outage
// exactly rather than approximating it: one kills the restart script in
// the window between freeing the port and starting the daemon, the other
// kills the process group of a caller that invoked the restart in the
// foreground. Neither is allowed a human in the loop afterwards.
//
// Everything runs against a scratch port, a scratch HOME and a stub
// daemon, because the subject matter is the restart path itself and a
// botched exercise of it takes the daily fleet down. The daily port is
// refused outright.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// dailyPort is the one port these tests must never touch.
const dailyPort = 13705

// rig is one isolated universe: a scratch port, a scratch HOME, a stub
// daemon binary, and a blurter shim that records notices instead of
// sending them.
type rig struct {
	t          *testing.T
	root       string // repo root
	dir        string // scratch state
	home       string
	port       int
	stub       string // stub daemon binary
	watchdog   string // built cmd/jevons-watchdog
	script     string // scripts/restart-daily-jevonsd.sh
	blurterLog string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t405RepoRoot(t)
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".jevons"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := &rig{
		t:          t,
		root:       root,
		dir:        dir,
		home:       home,
		port:       t405FreePort(t),
		script:     filepath.Join(root, "scripts", "restart-daily-jevonsd.sh"),
		blurterLog: filepath.Join(dir, "blurter.log"),
	}
	r.stub = t405BuildStubDaemon(t, dir)
	r.watchdog = filepath.Join(dir, "jevons-watchdog")
	t405Build(t, root, r.watchdog, "./cmd/jevons-watchdog")
	// The script re-execs through these two at fixed paths under the repo.
	// Prebuilt here with the real build cache: left to the script they
	// would compile under the scratch HOME, from cold.
	t405Build(t, root, filepath.Join(root, "bin", "detach"), "./cmd/detach")
	t405Build(t, root, filepath.Join(root, "bin", "runlock"), "./cmd/runlock")
	r.writeBlurterShim()

	// Whatever a test leaves listening is ours and must not outlive it.
	t.Cleanup(r.killDaemon)
	return r
}

// writeBlurterShim puts a fake blurter first on PATH. The watchdog
// notifies through the real one, which would page the owner from a test
// run; this records the call instead, which is also how the test asserts
// the owner was told.
func (r *rig) writeBlurterShim() {
	bin := filepath.Join(r.dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		r.t.Fatal(err)
	}
	shim := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >>%q\n", r.blurterLog)
	if err := os.WriteFile(filepath.Join(bin, "blurter"), []byte(shim), 0o755); err != nil {
		r.t.Fatal(err)
	}
}

// env is the isolated universe every child process runs in.
func (r *rig) env(extra ...string) []string {
	e := []string{
		"HOME=" + r.home,
		"PATH=" + filepath.Join(r.dir, "bin") + ":" + os.Getenv("PATH"),
		// A scratch HOME would otherwise mean a cold build cache for any
		// go build the script decides it needs.
		"GOCACHE=" + t405GoEnv(r.t, "GOCACHE"),
		"GOMODCACHE=" + t405GoEnv(r.t, "GOMODCACHE"),
		fmt.Sprintf("JEVONS_RESTART_PORT=%d", r.port),
		"JEVONS_RESTART_BIN=" + r.stub,
		"JEVONS_RESTART_SKIP_MAKE=1",
		"JEVONS_RESTART_WORKDIR=" + r.dir,
		"JEVONS_RESTART_LOG=" + filepath.Join(r.dir, "daemon.log"),
		"JEVONS_RESTART_DETACH_LOG=" + r.restartLog(),
		"JEVONS_RESTART_LOCK=" + filepath.Join(r.dir, "restart.lock"),
		"JEVONS_RESTART_STAMP=" + filepath.Join(r.dir, "restart.last"),
		"JEVONS_RESTART_ACTIVE=" + filepath.Join(r.dir, "restart.active"),
		"JEVONS_RESTART_WAIT_SEC=30",
		"JEVONS_RESTART_MIN_INTERVAL_SEC=0",
	}
	return append(e, extra...)
}

func (r *rig) restartLog() string { return filepath.Join(r.dir, "restart-daily.log") }

func (r *rig) stateDir() string { return supervise.Dir(r.dir) }

// serving asks the same question the watchdog asks.
func (r *rig) serving() bool {
	c := &http.Client{Timeout: time.Second}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/health", r.port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (r *rig) waitServing(want bool, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if r.serving() == want {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return r.serving() == want
}

// restart runs the script the way a fleet agent does, and waits for it.
func (r *rig) restart(extraEnv ...string) error {
	cmd := exec.Command(r.script, "--force")
	cmd.Dir = r.root
	cmd.Env = r.env(extraEnv...)
	out, err := cmd.CombinedOutput()
	r.t.Logf("restart script: %v\n%s", err, t405Tail(string(out), 15))
	return err
}

// cycle runs exactly one watchdog invocation, which is what launchd does.
func (r *rig) cycle(grace time.Duration) string {
	cmd := exec.Command(r.watchdog,
		"-port", fmt.Sprint(r.port),
		"-repo", r.root,
		"-state", r.dir,
		"-grace", grace.String())
	cmd.Dir = r.root
	cmd.Env = r.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("watchdog cycle failed: %v\n%s", err, out)
	}
	r.t.Logf("watchdog: %s", strings.TrimSpace(string(out)))
	return string(out)
}

func (r *rig) killDaemon() {
	out, _ := exec.Command("lsof", "-nP", "-iTCP:"+fmt.Sprint(r.port), "-sTCP:LISTEN", "-t").Output()
	for pid := range strings.FieldsSeq(string(out)) {
		_ = exec.Command("kill", "-9", pid).Run()
	}
}

func (r *rig) blurts() []string {
	b, err := os.ReadFile(r.blurterLog)
	if err != nil {
		return nil
	}
	var out []string
	for l := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestWatchdogRecoversARestartThatDiedMidBounce is the first half of the
// 🎯T405 oracle: the script is killed in the window between freeing the
// port and starting the replacement — the exact window it died in on
// 2026-08-10 — and the daemon must come back with nobody doing anything.
//
// No human action appears in this test after the fault. The only thing
// that runs is the watchdog, on the interval launchd runs it on.
func TestWatchdogRecoversARestartThatDiedMidBounce(t *testing.T) {
	r := newRig(t)

	if err := r.restart(); err != nil {
		t.Fatalf("baseline restart failed: %v", err)
	}
	if !r.waitServing(true, 30*time.Second) {
		t.Fatalf("stub daemon never came up on :%d", r.port)
	}

	// The fault: die after kill_port_listeners, before start_daemon_detached.
	if err := r.restart("JEVONS_RESTART_FAULT=after-kill"); err == nil {
		t.Fatal("fault-injected restart reported success; the seam did not fire")
	}
	if !r.waitServing(false, 10*time.Second) {
		t.Fatal("fault-injected restart left the port serving; it did not reproduce the outage")
	}

	// Cycle one: the outage is seen, not yet acted on. A supervisor that
	// restarted on first sight would fire into the middle of every
	// legitimate bounce.
	if out := r.cycle(0); !strings.Contains(out, "action=none") {
		t.Fatalf("first cycle acted on a single unserved observation:\n%s", out)
	}
	if r.serving() {
		t.Fatal("port came back without the watchdog acting; the test proves nothing")
	}

	// Cycle two: past the grace window, so the watchdog restarts.
	if out := r.cycle(0); !strings.Contains(out, "action=restart") {
		t.Fatalf("second cycle did not restart:\n%s", out)
	}
	if !r.waitServing(true, 60*time.Second) {
		t.Fatalf("daemon still not serving on :%d after the watchdog restarted it", r.port)
	}

	// Cycle three: recovery is observed and recorded.
	if out := r.cycle(0); !strings.Contains(out, "action=recovered") {
		t.Fatalf("third cycle did not close the outage:\n%s", out)
	}

	st, err := supervise.LoadState(r.stateDir())
	if err != nil {
		t.Fatal(err)
	}
	if st.Down() || st.Attempts != 0 {
		t.Fatalf("recovery left the outage open: %+v", st)
	}

	outages, err := supervise.LoadOutages(r.stateDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(outages) != 1 {
		t.Fatalf("recorded %d outages, want 1: %+v", len(outages), outages)
	}
	if outages[0].Attempts != 1 {
		t.Errorf("outage records %d attempts, want 1: %+v", outages[0].Attempts, outages[0])
	}

	// The owner hears about it twice over: out of band while it is down …
	blurts := r.blurts()
	if len(blurts) == 0 {
		t.Fatal("the owner was told nothing out of band about the outage")
	}
	if !strings.Contains(strings.Join(blurts, "\n"), "--severity problem") {
		t.Errorf("no problem-severity notice while the daemon was down:\n%s", strings.Join(blurts, "\n"))
	}

	// … and in the cockpit once it is back, which is where they already
	// look. The daemon reports it, because the watchdog cannot: during the
	// outage there is nothing to write a chat line into.
	var told []string
	notify := func(subject, kind, text string) bool {
		if subject != supervise.OutageSubject || kind != supervise.OutageKind {
			t.Errorf("notice filed as %q/%q", subject, kind)
		}
		told = append(told, text)
		return true
	}
	n, err := supervise.ReportPending(r.stateDir(), notify)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(told) != 1 {
		t.Fatalf("reported %d outages to the owner, want 1", n)
	}
	for _, want := range []string{"outage", "watchdog", "no owner action"} {
		if !strings.Contains(told[0], want) {
			t.Errorf("owner notice missing %q: %s", want, told[0])
		}
	}

	// And exactly once, however often the daemon restarts afterwards.
	n, err = supervise.ReportPending(r.stateDir(), notify)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(told) != 1 {
		t.Fatalf("the same outage was reported twice (n=%d, told=%d)", n, len(told))
	}
}

// TestForegroundRestartSurvivesItsCallerBeingKilled is the second half:
// the restart is invoked the wrong way — in the foreground, by a caller
// that is then killed outright — and it still finishes.
//
// This is the failure that took the fleet down. The script had documented
// "invoke me detached" since its first version, and a correctness
// property that depends on every caller remembering a convention is not a
// property. So the caller here does everything wrong on purpose: no
// nohup, no setsid, and a SIGKILL to the whole process group, which is
// worse than the SIGTERM an agent's death actually delivers.
func TestForegroundRestartSurvivesItsCallerBeingKilled(t *testing.T) {
	t405ForegroundKill(t, true)
}

// TestForegroundRestartDiesWithoutSelfDetach is the control that gives the
// test above its meaning. Run the identical scenario with the self-detach
// turned off and the bounce dies with its caller — which is both the
// 2026-08-10 outage reproduced and proof that the passing test is
// measuring detachment rather than a race it happens to win.
func TestForegroundRestartDiesWithoutSelfDetach(t *testing.T) {
	t405ForegroundKill(t, false)
}

func t405ForegroundKill(t *testing.T, detached bool) {
	t.Helper()
	r := newRig(t)

	if r.serving() {
		t.Fatalf("something is already listening on scratch port %d", r.port)
	}

	cmd := exec.Command(r.script, "--force")
	cmd.Dir = r.root
	if detached {
		cmd.Env = r.env()
	} else {
		cmd.Env = r.env("JEVONS_RESTART_NO_DETACH=1")
	}
	// The caller's own output, so the marker below is readable whether or
	// not the run detached.
	callerLog := filepath.Join(r.dir, "caller.log")
	lf, err := os.Create(callerLog)
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	cmd.Stdout, cmd.Stderr = lf, lf
	// Its own process group, so the kill below is precisely the sweep that
	// takes down an agent and everything it started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid

	// Wait until the bounce is genuinely under way — killing before the
	// re-exec would prove nothing either way — then kill the caller.
	if !t405WaitForLog(callerLog, "restart-daily-jevonsd: root=", 60*time.Second) {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		t.Fatalf("restart never started; log:\n%s", t405ReadFile(callerLog))
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		t.Fatalf("killing the caller's process group: %v", err)
	}
	waitErr := cmd.Wait()
	t.Logf("caller died: %v", waitErr)
	if waitErr == nil {
		t.Fatal("the caller exited normally; it was not killed and the test proves nothing")
	}

	if !detached {
		// The control. Without the re-exec the bounce is inside the group
		// that was just swept, so the port stays dead — the outage as it
		// actually happened.
		if r.waitServing(true, 10*time.Second) {
			t.Fatal("a non-detached bounce survived its caller's SIGKILL; this control no longer reproduces the outage")
		}
		return
	}

	// Nobody is left to finish the job. It finishes anyway.
	if !r.waitServing(true, 60*time.Second) {
		t.Fatalf("the bounce died with its caller: :%d never came up\ncaller log:\n%s\nrestart log:\n%s",
			r.port, t405Tail(t405ReadFile(callerLog), 20), t405Tail(t405ReadFile(r.restartLog()), 40))
	}

	// And what is serving is genuinely outside the killed group — a
	// survivor still inside it would be luck, not detachment.
	out, err := exec.Command("ps", "-o", "pgid=", "-p", fmt.Sprint(t405ListenPID(t, r.port))).Output()
	if err == nil {
		if got := strings.TrimSpace(string(out)); got == fmt.Sprint(pgid) {
			t.Errorf("the daemon is in the caller's process group %s; it survived by luck", got)
		}
	}
}

// --- helpers ---------------------------------------------------------

func t405RepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func t405FreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	if port == dailyPort {
		t.Fatalf("refusing to test against the daily port %d", dailyPort)
	}
	return port
}

func t405GoEnv(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}

func t405Build(t *testing.T, root, out, pkg string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
}

// t405BuildStubDaemon compiles something that answers the two questions
// the restart script asks of a daemon and nothing else. The real jevonsd
// would drag its whole state directory into a test about process
// lifetimes.
func t405BuildStubDaemon(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "stub")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	const prog = `package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := flag.Int("port", 0, "")
	flag.String("workdir", "", "")
	flag.Parse()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/api/frontier", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "[]") })
	if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *port), mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module stubdaemon\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "stubdaemon")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = src
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the stub daemon: %v\n%s", err, b)
	}
	return bin
}

func t405WaitForLog(path, want string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if strings.Contains(t405ReadFile(path), want) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func t405ReadFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func t405ListenPID(t *testing.T, port int) int {
	t.Helper()
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+fmt.Sprint(port), "-sTCP:LISTEN", "-t").Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	var pid int
	if _, err := fmt.Sscan(fields[0], &pid); err != nil {
		return 0
	}
	return pid
}

func t405Tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

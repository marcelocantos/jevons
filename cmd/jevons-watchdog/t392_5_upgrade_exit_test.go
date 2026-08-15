// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// The 🎯T392.5 oracle: a daemon restart must not cancel the agent turns
// that are in flight when it lands.
//
// The 🎯T392 baseline measured 120 of 1,070 turns cancelled (11.2%),
// burning 59.7M input tokens (6.6%) that produced nothing, and named
// daemon restarts landing mid-flight as one of the two dominant sources.
// The restarts themselves are correct and must stay routine and
// unattended (🎯T188/🎯T191) — what was wrong is that each one charged
// the fleet a full context for work it then threw away.
//
// What decides that is a single signal from the restart script, because
// jevonsd has told two shutdowns apart since 🎯T40:
//
//	SIGINT/SIGTERM → ModeNormal  → registry.StopAll(): every agent
//	                               stopped, every turn in flight lost.
//	SIGHUP         → ModeUpgrade → StopAll skipped, reattach handles
//	                               written, agents left running.
//
// So the stub daemon here implements exactly that fork and nothing else,
// and starts a detached "agent turn" in its own process group the way
// jevonsd starts `grok agent serve` (CLAUDIA_GROK_CONNECT=1) — a process
// that does not die with its coordinator, so the ONLY thing that decides
// whether the turn survives is whether the coordinator stopped it on the
// way out. The real jevonsd would drag its whole state directory into a
// test about process lifetimes.
//
// Three tests, each other's controls:
//
//   - the shipped script bounces the daemon and the turn is still running
//     afterwards, with a reattach handle written for it;
//   - the line this change deleted (`kill $pids`, a plain SIGTERM) kills
//     the same turn under the same rig, which is the regression the first
//     test is claiming to detect;
//   - a daemon that ignores SIGHUP still loses the port inside the grace
//     window, because freeing it is not optional (🎯T194) and an upgrade
//     exit is a request, not a drain.
//
// Everything runs against a scratch port, a scratch HOME and the stub;
// the daily port is refused outright by t405FreePort.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// t3925Rig is the 🎯T405 rig with the stub daemon swapped for one that
// models the shutdown fork and carries an agent turn across the bounce.
type t3925Rig struct {
	*rig
	state string // where the stub records what it did
}

func newT3925Rig(t *testing.T) *t3925Rig {
	t.Helper()
	r := newRig(t)
	state := filepath.Join(r.dir, "stubstate")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	r.stub = t3925BuildStubDaemon(t, r.dir, t3925BuildStubAgent(t, r.dir), state)
	return &t3925Rig{rig: r, state: state}
}

// agentPID is the turn that was in flight, as recorded by the daemon that
// started it. Read it BEFORE bouncing: the successor starts a turn of its
// own and overwrites the file.
func (r *t3925Rig) agentPID(t *testing.T) int {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join(r.state, "agent.pid"))
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the stub daemon never recorded an agent turn in %s", r.state)
	return 0
}

// running asks whether that turn is still alive. Signal 0 is the question
// without the consequence.
func t3925Running(pid int) bool { return syscall.Kill(pid, 0) == nil }

func (r *t3925Rig) waitGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !t3925Running(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !t3925Running(pid)
}

// TestT392_5RestartLeavesTheTurnInFlightRunning is the oracle proper: the
// shipped restart script bounces a serving daemon, and the turn that was
// running when it landed is still running when it is over — and has a
// reattach handle waiting for the successor.
func TestT392_5RestartLeavesTheTurnInFlightRunning(t *testing.T) {
	r := newT3925Rig(t)

	if err := r.restart(); err != nil {
		t.Fatalf("baseline restart failed: %v", err)
	}
	if !r.waitServing(true, 30*time.Second) {
		t.Fatalf("stub daemon never came up on :%d", r.port)
	}
	pid := r.agentPID(t)
	if !t3925Running(pid) {
		t.Fatalf("the agent turn (pid %d) was not running before the bounce; the test proves nothing", pid)
	}

	// The bounce a fleet agent triggers on every daemon-path land.
	if err := r.restart(); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	if !r.waitServing(true, 60*time.Second) {
		t.Fatalf("the bounce left :%d dead:\n%s", r.port, t405Tail(t405ReadFile(r.restartLog()), 40))
	}

	if !t3925Running(pid) {
		t.Fatalf("the restart cancelled the turn in flight (pid %d is gone) — 🎯T392.5 has regressed:\n%s",
			pid, t405Tail(t405ReadFile(r.restartLog()), 40))
	}
	if stopped := filepath.Join(r.state, "stopall"); t405ReadFile(stopped) != "" {
		t.Errorf("the daemon took the drain path (%s exists); the script did not ask for an upgrade exit", stopped)
	}
	handles := t405ReadFile(filepath.Join(r.state, "handles.json"))
	if !strings.Contains(handles, strconv.Itoa(pid)) {
		t.Errorf("no reattach handle written for the surviving turn: %q", handles)
	}
	// The successor is a different process — a "bounce" that never
	// replaced the daemon would pass everything above for free.
	if got := t405ListenPID(t, r.port); got == 0 {
		t.Error("nothing is listening after the restart")
	}
}

// TestT392_5DrainCancelsTheTurn is the control that gives the test above
// its meaning: the line this change deleted — a plain `kill`, which is
// SIGTERM — run against the same rig, killing the same turn. Without it,
// a passing oracle could be measuring a stub that never stops anything.
func TestT392_5DrainCancelsTheTurn(t *testing.T) {
	r := newT3925Rig(t)

	if err := r.restart(); err != nil {
		t.Fatalf("baseline restart failed: %v", err)
	}
	if !r.waitServing(true, 30*time.Second) {
		t.Fatalf("stub daemon never came up on :%d", r.port)
	}
	pid := r.agentPID(t)

	// Verbatim the old kill_port_listeners body.
	daemon := t405ListenPID(t, r.port)
	if daemon == 0 {
		t.Fatal("no listener to signal")
	}
	if err := syscall.Kill(daemon, syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM to the daemon: %v", err)
	}

	if !r.waitGone(pid, 20*time.Second) {
		t.Fatalf("the drain path left the turn (pid %d) alive; this control no longer reproduces the loss", pid)
	}
	if t405ReadFile(filepath.Join(r.state, "stopall")) == "" {
		t.Error("the daemon did not record a StopAll; the control did not exercise the drain")
	}
}

// TestT392_5WedgedUpgradeExitStillFreesThePort holds the other side of
// the trade. An upgrade exit is a request; freeing the port is not
// optional, because a restart that reports success while the old binary
// serves is the failure the script exists to prevent (🎯T194). A daemon
// that ignores SIGHUP must still be gone, and the replacement serving,
// inside the grace window.
func TestT392_5WedgedUpgradeExitStillFreesThePort(t *testing.T) {
	r := newT3925Rig(t)

	if err := r.restart("STUB_IGNORE_HUP=1"); err != nil {
		t.Fatalf("baseline restart failed: %v", err)
	}
	if !r.waitServing(true, 30*time.Second) {
		t.Fatalf("stub daemon never came up on :%d", r.port)
	}
	wedged := t405ListenPID(t, r.port)
	if wedged == 0 {
		t.Fatal("no listener to wedge")
	}

	start := time.Now()
	if err := r.restart("STUB_IGNORE_HUP=1", "JEVONS_RESTART_STOP_WAIT_SEC=3"); err != nil {
		t.Fatalf("restart against a wedged daemon failed: %v\n%s", err, t405Tail(t405ReadFile(r.restartLog()), 40))
	}
	if !r.waitServing(true, 60*time.Second) {
		t.Fatalf("a wedged upgrade exit left :%d dead:\n%s", r.port, t405Tail(t405ReadFile(r.restartLog()), 40))
	}
	if t3925Running(wedged) {
		t.Errorf("the wedged daemon (pid %d) survived the restart; the port was never really freed", wedged)
	}
	// The escalation must not be a fixed sleep the common case also pays:
	// a 3s window plus a health wait, generously bounded.
	if elapsed := time.Since(start); elapsed > 45*time.Second {
		t.Errorf("the restart took %s against a 3s stop window; the escalation is not bounded", elapsed)
	}
}

// --- the stub ---------------------------------------------------------

// t3925BuildStubAgent compiles the turn in flight: a process that does
// nothing but stay alive long enough to be counted, started detached by
// the stub daemon so that only a deliberate stop can end it.
func t3925BuildStubAgent(t *testing.T, dir string) string {
	t.Helper()
	const prog = `package main

import "time"

func main() { time.Sleep(10 * time.Minute) }
`
	return t3925BuildProg(t, dir, "stubagent", prog)
}

// t3925BuildStubDaemon compiles a daemon that answers the two questions
// the restart script asks, starts one detached agent turn, and forks its
// shutdown the way jevonsd does: SIGHUP writes reattach handles and
// leaves the turn running, SIGINT/SIGTERM stop it.
func t3925BuildStubDaemon(t *testing.T, dir, agent, state string) string {
	t.Helper()
	prog := fmt.Sprintf(`package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

const (
	agentBin = %q
	stateDir = %q
)

func main() {
	port := flag.Int("port", 0, "")
	flag.String("workdir", "", "")
	flag.Parse()

	// The turn in flight. Setpgid puts it outside this process's group,
	// exactly as jevonsd's detached `+"`grok agent serve`"+` children are, so
	// nothing kills it by accident and the test measures intent.
	turn := exec.Command(agentBin)
	turn.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := turn.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "starting the agent turn:", err)
		os.Exit(1)
	}
	pid := turn.Process.Pid
	if err := os.WriteFile(filepath.Join(stateDir, "agent.pid"), []byte(fmt.Sprint(pid)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// One signal is read and the handler returns, as the daemon's does:
	// a second signal after the first is never seen.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-sigCh
		if sig == syscall.SIGHUP {
			if os.Getenv("STUB_IGNORE_HUP") == "1" {
				return // wedged: the port is only freed by SIGKILL
			}
			_ = os.WriteFile(filepath.Join(stateDir, "handles.json"),
				[]byte(fmt.Sprintf("{\"agents\":[%%d]}", pid)), 0o644)
			os.Exit(0)
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_ = os.WriteFile(filepath.Join(stateDir, "stopall"), []byte(sig.String()), 0o644)
		os.Exit(0)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/api/frontier", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "[]") })
	if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%%d", *port), mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`, agent, state)
	return t3925BuildProg(t, dir, "t3925stubdaemon", prog)
}

func t3925BuildProg(t *testing.T, dir, name, prog string) string {
	t.Helper()
	src := filepath.Join(dir, "src-"+name)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module "+name+"\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = src
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", name, err, b)
	}
	return bin
}

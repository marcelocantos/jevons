// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// The 🎯T1086 oracle: restart-jevonsd's supervised upgrade path, against a
// real supervisord.
//
// On 2026-10-20 the script SIGHUPed a supervisord program whose config had
// autorestart=false (left by a pause), then waited 90 s for a daemon nothing
// would start, and left the development daemon down. These tests replay that
// sequence on a private supervisord — its own config, socket and logs in a
// temp dir, driven only through `supervisorctl -c <that config>` — with a
// stub daemon on a scratch port. The owner's supervisord, its supervisor.d
// and the development port are never touched.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// t1086Supervisord is one private supervisord.
type t1086Supervisord struct {
	t    *testing.T
	conf string
	cmd  *exec.Cmd
}

// ctl runs supervisorctl against this supervisord only.
func (s *t1086Supervisord) ctl(args ...string) (string, error) {
	out, err := exec.Command("supervisorctl", append([]string{"-c", s.conf}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (s *t1086Supervisord) programPID() int {
	out, _ := s.ctl("pid", "jevonsd")
	var pid int
	if _, err := fmt.Sscan(out, &pid); err != nil {
		return 0
	}
	return pid
}

// t1086StartSupervisord writes a private supervisord config with one
// program, jevonsd, running command on the rig's scratch port, and starts
// supervisord in the foreground as a child of the test.
func t1086StartSupervisord(t *testing.T, r *rig, autorestart, command string) *t1086Supervisord {
	t.Helper()
	for _, bin := range []string{"supervisord", "supervisorctl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed; the 🎯T1086 oracle needs a real supervisord", bin)
		}
	}
	// A unix socket path is capped near 104 bytes on macOS, and test temp
	// dirs run long, so the socket gets a short directory of its own.
	sockDir, err := os.MkdirTemp("", "t1086-")
	if err != nil {
		t.Fatal(err)
	}
	if len(sockDir) > 80 {
		os.RemoveAll(sockDir)
		if sockDir, err = os.MkdirTemp("/tmp", "t1086-"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s.sock")

	supDir := filepath.Join(r.dir, "supervisor")
	if err := os.MkdirAll(filepath.Join(supDir, "supervisor.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(supDir, "supervisord.conf")
	// The owner's layout in miniature: a main config that includes
	// supervisor.d/*.ini, where the program's own file lives.
	mainConf := fmt.Sprintf(`[unix_http_server]
file=%[1]s

[supervisord]
logfile=%[2]s/supervisord.log
pidfile=%[2]s/supervisord.pid
childlogdir=%[2]s
nodaemon=true

[rpcinterface:supervisor]
supervisor.rpcinterface_factory = supervisor.rpcinterface:make_main_rpcinterface

[supervisorctl]
serverurl=unix://%[1]s

[include]
files = supervisor.d/*.ini
`, sock, supDir)
	// The 2026-10-20 program: paused, so not started by supervisord, and
	// started by hand afterwards.
	program := fmt.Sprintf(`[program:jevonsd]
command=%s
autostart=false
autorestart=%s
startsecs=1
startretries=1
stopwaitsecs=5
stdout_logfile=%s/jevonsd.log
redirect_stderr=true
`, command, autorestart, supDir)
	if err := os.WriteFile(conf, []byte(mainConf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(supDir, "supervisor.d", "jevonsd.ini"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}

	// The script reads the policy through bin/supervisorpolicy; prebuilt
	// with the real build cache, as newRig does for detach and runlock.
	t405Build(t, r.root, filepath.Join(r.root, "bin", "supervisorpolicy"), "./cmd/supervisorpolicy")

	s := &t1086Supervisord{t: t, conf: conf}
	s.cmd = exec.Command("supervisord", "-n", "-c", conf)
	s.cmd.Dir = supDir
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = s.cmd.Wait(); close(exited) }()
	t.Cleanup(func() {
		_, _ = s.ctl("shutdown")
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = s.cmd.Process.Kill()
			<-exited
		}
		if t.Failed() {
			t.Logf("supervisord.log:\n%s", t405Tail(t405ReadFile(filepath.Join(supDir, "supervisord.log")), 30))
			t.Logf("jevonsd.log:\n%s", t405Tail(t405ReadFile(filepath.Join(supDir, "jevonsd.log")), 20))
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	for {
		if out, err := s.ctl("pid"); err == nil && out != "" && out != "0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("private supervisord never answered on %s", sock)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The owner had started the paused program by hand; so does the test.
	if out, err := s.ctl("start", "jevonsd"); err != nil {
		t.Fatalf("supervisorctl start jevonsd: %v\n%s", err, out)
	}
	if !r.waitServing(true, 15*time.Second) {
		t.Fatal("supervised stub never served")
	}
	if got, want := t405ListenPID(t, r.port), s.programPID(); got == 0 || got != want {
		t.Fatalf("listener pid %d is not supervisord's program pid %d", got, want)
	}
	return s
}

// t1086BuildUpgradeStub compiles a daemon double that serves /health and
// /api/frontier and treats SIGHUP as jevonsd does: an upgrade exit with
// status 0, which supervisord counts as expected. With -once MARKER, a
// second launch (MARKER exists) never listens: the replacement that never
// serves.
func t1086BuildUpgradeStub(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "upgradestub")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	const prog = `package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	port := flag.Int("port", 0, "")
	once := flag.String("once", "", "")
	flag.Parse()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	if *once != "" {
		if _, err := os.Stat(*once); err == nil {
			<-hup
			os.Exit(0)
		}
		_ = os.WriteFile(*once, []byte("served\n"), 0o644)
	}
	go func() {
		<-hup
		os.Exit(0)
	}()
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
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module upgradestub\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "upgradestub-bin")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the upgrade stub: %v\n%s", err, b)
	}
	return bin
}

// t1086Restart runs the script against the private supervisord.
func t1086Restart(r *rig, s *t1086Supervisord, waitSec int) error {
	return r.restart(
		"JEVONS_RESTART_SUPERVISOR_CONF="+s.conf,
		fmt.Sprintf("JEVONS_RESTART_WAIT_SEC=%d", waitSec),
		"JEVONS_RESTART_STOP_WAIT_SEC=5",
		"JEVONS_RESTART_RELAUNCH_WAIT_SEC=10",
	)
}

// TestT1086UpgradeUnderNonRelaunchingSupervisorLeavesDaemonUp is the
// 2026-10-20 sequence: autorestart=false, program running, upgrade bounce.
// The daemon must be serving afterwards from a new process that supervisord
// owns, and the script must have named the policy before signalling.
func TestT1086UpgradeUnderNonRelaunchingSupervisorLeavesDaemonUp(t *testing.T) {
	r := newRig(t)
	stub := t1086BuildUpgradeStub(t, r.dir)
	s := t1086StartSupervisord(t, r, "false", fmt.Sprintf("%s -port %d", stub, r.port))
	before := s.programPID()

	if err := t1086Restart(r, s, 30); err != nil {
		t.Fatalf("restart under autorestart=false failed: %v", err)
	}
	if !r.serving() {
		t.Fatal("daemon down after the bounce: nothing relaunched it")
	}
	after := s.programPID()
	if after == 0 || after == before {
		t.Fatalf("supervisord program pid before=%d after=%d; want a new supervised process", before, after)
	}
	if got := t405ListenPID(t, r.port); got != after {
		t.Fatalf("listener pid %d is not supervisord's program pid %d", got, after)
	}
	log := t405ReadFile(r.restartLog())
	policyAt := strings.Index(log, "will not relaunch jevonsd on an upgrade exit (autorestart=false")
	signalAt := strings.Index(log, "React upgrade through existing supervisord owner")
	if policyAt < 0 || signalAt < 0 || policyAt < signalAt {
		t.Fatalf("restart log does not name autorestart=false at the signal step:\n%s", t405Tail(log, 30))
	}
	if !strings.Contains(log, "starting it") || !strings.Contains(log, "started jevonsd through supervisord") {
		t.Fatalf("restart log does not show the script starting the program:\n%s", t405Tail(log, 30))
	}
	if b := r.blurts(); len(b) != 0 {
		t.Fatalf("owner notified on a bounce that succeeded: %q", b)
	}
}

// The control: under autorestart=true supervisord relaunches the program
// itself and the script does not start a second one.
func TestT1086UpgradeUnderRelaunchingSupervisorLeavesItToSupervisord(t *testing.T) {
	r := newRig(t)
	stub := t1086BuildUpgradeStub(t, r.dir)
	s := t1086StartSupervisord(t, r, "true", fmt.Sprintf("%s -port %d", stub, r.port))
	before := s.programPID()

	if err := t1086Restart(r, s, 30); err != nil {
		t.Fatalf("restart under autorestart=true failed: %v", err)
	}
	if !r.serving() {
		t.Fatal("daemon down after the bounce")
	}
	if after := s.programPID(); after == 0 || after == before {
		t.Fatalf("supervisord program pid before=%d after=%d", before, after)
	}
	log := t405ReadFile(r.restartLog())
	if !strings.Contains(log, "supervisord relaunches jevonsd on exit") || !strings.Contains(log, "supervisord relaunched jevonsd: pid=") {
		t.Fatalf("restart log does not show supervisord relaunching:\n%s", t405Tail(log, 30))
	}
	if strings.Contains(log, "starting it") {
		t.Fatalf("script started a program supervisord was relaunching:\n%s", t405Tail(log, 30))
	}
}

// When the replacement never serves, the timeout tells the owner out of
// band before the script exits non-zero.
func TestT1086TimeoutTellsTheOwner(t *testing.T) {
	r := newRig(t)
	stub := t1086BuildUpgradeStub(t, r.dir)
	marker := filepath.Join(r.dir, "served-once")
	s := t1086StartSupervisord(t, r, "false", fmt.Sprintf("%s -port %d -once %s", stub, r.port, marker))

	if err := t1086Restart(r, s, 3); err == nil {
		t.Fatal("restart reported success with a replacement that never served")
	}
	var notice string
	for _, b := range r.blurts() {
		if strings.Contains(b, "--severity problem") && strings.Contains(b, fmt.Sprintf("jevons daemon down on :%d", r.port)) {
			notice = b
		}
	}
	if notice == "" {
		t.Fatalf("no out-of-band down notice; blurts=%q\nlog:\n%s", r.blurts(), t405Tail(t405ReadFile(r.restartLog()), 20))
	}
	if !strings.Contains(notice, "not serving 3s after the bounce") {
		t.Fatalf("notice does not say why: %q", notice)
	}
}

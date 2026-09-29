// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The a80d7b58 isolate log's own last two records (🎯T839 evidence).
const t839IsolateLog = `time=2026-09-22T09:54:23.160+10:00 level=INFO msg="jevon agent" provider=grok session=0154990b resume=false
time=2026-09-22T09:54:23.221+10:00 level=ERROR msg="auto-start failed" agent=jevons err="exclusive GROK_HOME unavailable for session 0154990b: stat /tmp/x/claudia/grok-homes/0154990b: no such file or directory"
time=2026-09-22T09:54:23.221+10:00 level=ERROR msg="OVERSEER NOT RUNNING — chat cannot respond until this is fixed" overseer=jevons provider=grok
time=2026-09-22T09:54:23.300+10:00 level=INFO msg="agents watch started"
`

// fakeIsolate is a suite whose isolate liveness the test controls.
func fakeIsolate(t *testing.T) (*suite, *bytes.Buffer, *bool) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "jevonsd.log")
	if err := os.WriteFile(logPath, []byte(t839IsolateLog), 0o600); err != nil {
		t.Fatal(err)
	}
	alive := true
	var out bytes.Buffer
	s := &suite{logPath: logPath, stdout: &out}
	s.aliveProbe = func() error {
		if alive {
			return nil
		}
		return errors.New("isolate not answering on 127.0.0.1:64859: connect: connection refused")
	}
	return s, &out, &alive
}

func refused(what string) func() error {
	return func() error {
		return fmt.Errorf("%s: dial tcp 127.0.0.1:64859: connect: connection refused", what)
	}
}

// TestT839CascadeIsOneOutage replays a80d7b58's shape: J13 fails with a live
// isolate (a real verdict), J17's restart kills the isolate, and every later
// journey hits the dead port — eleven of them as FAIL in the original run.
func TestT839CascadeIsOneOutage(t *testing.T) {
	s, out, alive := fakeIsolate(t)

	s.run("J13-overseer-migration", func() error {
		return errors.New("overseer on grok never recovered the codeword after migration")
	})
	s.run("J17-t418-queue-bounce", func() error {
		*alive = false
		return errors.New("restart: overseer not running yet: [{jevons stopped}]")
	})
	later := []string{"J18-t418-handover-mute", "J19-root-history-paint", "J22-send-once",
		"J23-fold-md", "J24-composer", "J25-fleet-sidebar", "J26-aside", "J27-frontier",
		"J28-ticker-chrome", "J21-goal-continue-all-backends", "J30-packaged-react-owner-turn",
		"J31-packaged-react-owner-boundary", "J32-packaged-react-send-cutin", "J33-undelivered-resend"}
	for _, name := range later {
		s.run(name, refused(name+" probe isolate GET /"))
	}
	// J19's probe fails as a node exit status, not a dial error: it must be
	// classified by the dead isolate, not by its text.
	s.run("J19b-paint-exit", func() error { return errors.New("j19 paint: exit status 1") })

	if s.failures != 1 {
		t.Fatalf("failures = %d, want 1 (J13 only; J17 is the outage)\n%s", s.failures, out)
	}
	if want := 1 + len(later) + 1; s.outages != want {
		t.Fatalf("outages = %d, want %d\n%s", s.outages, want, out)
	}
	if len(s.isolateOutages) != 1 {
		t.Fatalf("isolate outages = %d, want exactly one\n%s", len(s.isolateOutages), out)
	}
	o := s.isolateOutages[0]
	if o.journey != "J17-t418-queue-bounce" || o.later != len(later)+1 {
		t.Fatalf("outage = %+v, want J17 with %d later journeys", o, len(later)+1)
	}
	text := out.String()
	for _, want := range []string{
		"FAIL J13-overseer-migration",
		"OUT  J17-t418-queue-bounce  ISOLATE OUTAGE",
		"restart: overseer not running yet",
		`msg="auto-start failed"`,
		"OVERSEER NOT RUNNING",
		"OUT  J19-root-history-paint no isolate since J17-t418-queue-bounce",
		"OUT  J19b-paint-exit",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "ISOLATE OUTAGE") != 1 {
		t.Errorf("the outage is reported %d times, want once:\n%s", strings.Count(text, "ISOLATE OUTAGE"), text)
	}
	if strings.Count(text, "FAIL ") != 1 {
		t.Errorf("a journey without an isolate was scored FAIL:\n%s", text)
	}
}

// TestT839LiveIsolateFailureStaysFail is the control: a journey that fails
// while its isolate still answers is a verdict, whatever its error says.
func TestT839LiveIsolateFailureStaysFail(t *testing.T) {
	s, out, _ := fakeIsolate(t)
	s.run("J22-send-once", refused("j19 probe some other host"))
	s.run("J3-cancel-and-send", func() error {
		return errors.New("long turn: the request to cancel ended before it could be interrupted")
	})
	if s.failures != 2 || s.outages != 0 || len(s.isolateOutages) != 0 {
		t.Fatalf("failures=%d outages=%d isolate=%d, want 2/0/0\n%s",
			s.failures, s.outages, len(s.isolateOutages), out)
	}
}

// TestT839RecoveryEndsOutage: J20 and J29 restart the isolate. One that brings
// it back ends the outage, so its own genuine failure is FAIL again and the
// next death is a second, separately named outage.
func TestT839RecoveryEndsOutage(t *testing.T) {
	s, out, alive := fakeIsolate(t)
	s.run("J17-t418-queue-bounce", func() error { *alive = false; return errors.New("restart failed") })
	s.run("J20-plan-dest", func() error { *alive = true; return errors.New("plan destination wrong") })
	s.run("J21-goal-continue-all-backends", func() error { return nil })
	s.run("J29-tmux-anchor-spawn", func() error { *alive = false; return nil })
	s.run("J30-packaged-react-owner-turn", refused("j19 probe isolate GET /"))

	if s.failures != 1 || s.outages != 3 {
		t.Fatalf("failures=%d outages=%d, want 1 (J20) / 3 (J17, J29, J30)\n%s", s.failures, s.outages, out)
	}
	if len(s.isolateOutages) != 2 || s.isolateOutages[1].journey != "J29-tmux-anchor-spawn" {
		t.Fatalf("outages = %+v, want J17 then J29", s.isolateOutages)
	}
	if !strings.Contains(s.isolateOutages[1].cause, "passed but left the isolate down") {
		t.Errorf("J29 cause = %q", s.isolateOutages[1].cause)
	}
	if !strings.Contains(out.String(), "FAIL J20-plan-dest") ||
		!strings.Contains(out.String(), "isolate back after J20-plan-dest") {
		t.Errorf("recovery not reported:\n%s", out)
	}
}

// TestT839TeardownIsNotAnOutage: J5 runs after the suite stops the isolate.
func TestT839TeardownIsNotAnOutage(t *testing.T) {
	s, out, alive := fakeIsolate(t)
	*alive = false
	s.tornDown = true
	s.run("J5-isolation", func() error { return nil })
	if s.outages != 0 || s.failures != 0 || len(s.isolateOutages) != 0 {
		t.Fatalf("teardown scored as outage: %s", out)
	}
}

// TestT839IsolateAliveProbe drives the real probe: a missing process, a live
// process with a listener, a closed port, and an exited process.
func TestT839IsolateAliveProbe(t *testing.T) {
	s := &suite{}
	if err := s.isolateAlive(); err == nil || !strings.Contains(err.Error(), "last start or restart failed") {
		t.Fatalf("no process: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	s = &suite{host: ln.Addr().String(), cmd: cmd, cmdWait: wait}
	if err := s.isolateAlive(); err != nil {
		t.Fatalf("live isolate judged dead: %v", err)
	}

	ln.Close()
	if err := s.isolateAlive(); err == nil || !strings.Contains(err.Error(), "not answering") {
		t.Fatalf("closed port: %v", err)
	}

	_ = cmd.Process.Kill()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := s.isolateAlive()
		if err != nil && strings.Contains(err.Error(), "exited") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("exited process not detected: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s.cmd != nil || s.cmdWait != nil {
		t.Fatal("exited process left on the suite; signalStop would wait on it")
	}
}

func TestT839LastLogErrors(t *testing.T) {
	s, _, _ := fakeIsolate(t)
	got := lastLogErrors(s.logPath, 2)
	if !strings.Contains(got, `msg="auto-start failed"`) || !strings.Contains(got, "OVERSEER NOT RUNNING") ||
		strings.Contains(got, "time=") {
		t.Fatalf("lastLogErrors = %q", got)
	}
	if got := lastLogErrors(filepath.Join(t.TempDir(), "absent"), 2); got != "" {
		t.Fatalf("absent log = %q", got)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T379: the suite's MCP registration is global state on the owner's
// machine. These tests pin that it is given back on EVERY exit path, not
// just the one that unwinds — the leak that produced this target came from
// an abnormal exit, where a `defer` buys nothing.
//
// The abnormal paths end the process, so they are exercised in a subprocess:
// the test binary re-executes itself with JOURNEY_CLEANUP_CHILD set, the
// child registers a cleanup that writes a marker file and then dies the way
// the real program would, and the parent checks the marker and exit code.

const childEnv = "JOURNEY_CLEANUP_CHILD"

// TestMain lets this test binary act as its own subprocess fixture.
func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		os.Exit(m.Run())

	case "fatal":
		// Exactly the shape of the leak: register teardown, then die
		// through fatal() — which is what waitReady failure does.
		cleanups.Add(func() { touchMarker() })
		fatal(errors.New("simulated early failure"))

	case "signal":
		cleanups.Add(func() { touchMarker() })
		catchSignals()
		// Tell the parent we are ready to be signalled, then wait to be
		// killed. If the handler never fires this sleeps out and the
		// parent sees a timeout instead of a marker.
		fmt.Println("ready")
		time.Sleep(30 * time.Second)
		os.Exit(99)

	case "happy":
		cleanups.Add(func() { touchMarker() })
		exitNow(0)
	}
	os.Exit(97)
}

func touchMarker() {
	path := os.Getenv("JOURNEY_CLEANUP_MARKER")
	if path == "" {
		return
	}
	_ = os.WriteFile(path, []byte("cleaned"), 0o600)
}

// runChild re-executes this test binary in the given child mode and returns
// the exit code and whether the cleanup marker was written.
func runChild(t *testing.T, mode string, signalIt bool) (exitCode int, cleaned bool) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "marker")
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		childEnv+"="+mode,
		"JOURNEY_CLEANUP_MARKER="+marker,
	)

	if !signalIt {
		out, err := cmd.CombinedOutput()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("run child %s: %v (%s)", mode, err, out)
		}
		_, statErr := os.Stat(marker)
		return code, statErr == nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	// Wait for the child to install its handler before signalling, or the
	// signal can arrive before Notify and kill it outright.
	buf := make([]byte, len("ready\n"))
	if _, err := stdout.Read(buf); err != nil {
		t.Fatalf("child never signalled ready: %v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("signal child: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		_, statErr := os.Stat(marker)
		return code, statErr == nil
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child did not exit after SIGINT — signal handler missing")
		return 0, false
	}
}

// The path that actually leaked: an early failure ends the run through
// fatal(), whose os.Exit skips every defer.
func TestFatalExitStillTearsDown(t *testing.T) {
	code, cleaned := runChild(t, "fatal", false)
	if !cleaned {
		t.Error("fatal() exited without running teardown — the MCP registration leaks (🎯T379)")
	}
	if code != 1 {
		t.Errorf("fatal exit code = %d, want 1", code)
	}
}

// Ctrl-C during a long live journey is routine and must not cost the owner a
// permanent dead registration.
func TestInterruptStillTearsDown(t *testing.T) {
	code, cleaned := runChild(t, "signal", true)
	if !cleaned {
		t.Error("SIGINT killed the run without teardown — the MCP registration leaks (🎯T379)")
	}
	if code != 130 {
		t.Errorf("SIGINT exit code = %d, want 130 (128+SIGINT)", code)
	}
}

// The happy path must still tear down — and exit 0, so a clean run is not
// mistaken for a failure.
func TestNormalExitTearsDown(t *testing.T) {
	code, cleaned := runChild(t, "happy", false)
	if !cleaned {
		t.Error("normal exit skipped teardown")
	}
	if code != 0 {
		t.Errorf("normal exit code = %d, want 0", code)
	}
}

// Teardown runs once and in reverse order, so a second exit path arriving
// after the first (signal during shutdown) cannot double-remove.
func TestCleanupRunsOnceInReverseOrder(t *testing.T) {
	var c exitCleanup
	var order []string
	c.Add(func() { order = append(order, "first") })
	c.Add(func() { order = append(order, "second") })

	c.Run()
	c.Run() // idempotent: a signal arriving mid-teardown must not re-run

	if got := strings.Join(order, ","); got != "second,first" {
		t.Errorf("teardown order = %q, want %q (reverse registration, once)", got, "second,first")
	}
}

// A panicking teardown must not strand the ones behind it: giving back the
// MCP registration matters more than any single cleanup succeeding.
func TestPanickingCleanupDoesNotStrandTheRest(t *testing.T) {
	var c exitCleanup
	reached := false
	c.Add(func() { reached = true })
	c.Add(func() { panic("boom") })
	c.Run()
	if !reached {
		t.Error("a panicking teardown stranded the remaining ones")
	}
}

// Over-broadness guard: teardown removes the JOURNEY registration and never
// the daily one. An over-eager cleanup that removed `jevonsmcp` would take
// the owner's real overseer tools out with it — a far worse failure than the
// leak this target is about.
func TestTeardownTargetsOnlyTheJourneyRegistration(t *testing.T) {
	if mcpName == "jevonsmcp" {
		t.Fatal("journey MCP name must differ from the daily name")
	}
	if !strings.Contains(mcpName, "journey") {
		t.Errorf("journey MCP name %q should be self-evidently the journey one", mcpName)
	}
	for _, p := range []claudia.Provider{claudia.ProviderClaude, claudia.ProviderGrok} {
		args := mcpRemoveArgs(p, mcpName)
		if len(args) == 0 || args[len(args)-1] != mcpName {
			t.Errorf("%s remove args %v must end in the journey name", p, args)
		}
		for _, a := range args {
			if a == "jevonsmcp" {
				t.Errorf("%s remove args %v name the DAILY registration", p, args)
			}
		}
	}
}

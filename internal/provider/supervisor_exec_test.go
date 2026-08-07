// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package provider

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T27.3 launch-mode oracle against REAL processes. The fakes in
// lifecycle_test.go prove the reconciler's decisions; this file proves
// the supervisor actually spawns, restarts, and reaps an arbitrary
// non-Claude command — the generic-supervisor half of the acceptance
// that claudia (tmux + agent-CLI hardwired) cannot serve.
//
// The provider under test is this test binary re-executed in helper
// mode, so the oracle needs no fixture binary and no shell.

const (
	helperEnvMode = "JEVONS_TEST_PROVIDER_MODE"
	helperEnvLog  = "JEVONS_TEST_PROVIDER_LOG"

	helperModeServe = "serve" // run until signalled
	helperModeCrash = "crash" // exit immediately, non-zero
)

// TestProviderHelperProcess is not a real test: it is the provider
// process the supervisor tests launch. It no-ops unless the helper env
// var selects a mode.
func TestProviderHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnvMode)
	if mode == "" {
		t.Skip("helper process entry point; not a test")
	}
	if path := os.Getenv(helperEnvLog); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString("start\n")
			_ = f.Close()
		}
	}
	switch mode {
	case helperModeCrash:
		os.Exit(3)
	case helperModeServe:
		// Exit cleanly on SIGTERM so the test can tell a graceful reap
		// from an escalated SIGKILL.
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
		select {
		case <-ch:
			os.Exit(0)
		case <-time.After(2 * time.Minute):
			os.Exit(9) // safety valve: never outlive the test run
		}
	default:
		os.Exit(64)
	}
}

// helperDecl builds a launch declaration that re-executes this test
// binary. Which helper mode it runs comes from the launcher's env.
func helperDecl(t *testing.T, id string) config.ProviderDecl {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	return config.ProviderDecl{
		ID:        id,
		Transport: config.ProviderTransportLaunch,
		Params: map[string]any{
			"exec": self,
			"argv": []any{"-test.run=TestProviderHelperProcess"},
		},
	}
}

// execLifecycle wires a Lifecycle to a real ExecLauncher with the helper
// environment, fast backoff, and guaranteed teardown.
func execLifecycle(t *testing.T, mode, logPath string) *Lifecycle {
	t.Helper()
	env := append(os.Environ(), helperEnvMode+"="+mode)
	if logPath != "" {
		env = append(env, helperEnvLog+"="+logPath)
	}
	l := NewLifecycle(LifecycleArgs{
		Launcher: &ExecLauncher{Env: env},
		Backoff:  Backoff{Base: 10 * time.Millisecond, Max: 50 * time.Millisecond, Factor: 2},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := l.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	return l
}

// processAlive reports whether pid still exists (signal 0 probe). The
// supervisor Waits on its children, so a reaped provider is truly gone
// rather than a zombie that would still answer.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// countStarts reads the helper's append-only start log.
func countStarts(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read helper log: %v", err)
	}
	return strings.Count(string(raw), "start\n")
}

// TestExecSupervisorLaunchesAndReapsRealProcess: the generic supervisor
// spawns an arbitrary command, reports it healthy, and reaps it on
// disable — no Claude, no tmux, no agent CLI involved.
func TestExecSupervisorLaunchesAndReapsRealProcess(t *testing.T) {
	l := execLifecycle(t, helperModeServe, "")

	decl := helperDecl(t, "helper")
	if err := l.Reconcile([]config.ProviderDecl{decl}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	h := waitForPhase(t, l, "helper", PhaseRunning)
	if h.PID <= 0 {
		t.Fatalf("running provider has no PID: %+v", h)
	}
	if !processAlive(h.PID) {
		t.Fatalf("supervisor reports pid %d running, but it does not exist", h.PID)
	}
	pid := h.PID

	// Disable it: the process must actually die and leave health.
	disabled := decl
	disabled.Enable = boolPtr(false)
	if err := l.Reconcile([]config.ProviderDecl{disabled}); err != nil {
		t.Fatalf("reconcile disable: %v", err)
	}
	if got := len(l.Health()); got != 0 {
		t.Errorf("health has %d entries after disable, want 0", got)
	}
	waitFor(t, "real provider process reaped", func() bool { return !processAlive(pid) })
}

// TestExecSupervisorRestartsCrashLoop: a provider that keeps dying keeps
// being restarted, with backoff, and the restart count is observable.
func TestExecSupervisorRestartsCrashLoop(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "starts.log")
	l := execLifecycle(t, helperModeCrash, logPath)

	if err := l.Reconcile([]config.ProviderDecl{helperDecl(t, "flaky")}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	waitFor(t, "crash-looping provider restarted at least 3 times", func() bool {
		return countStarts(t, logPath) >= 3
	})

	var h Health
	for _, entry := range l.Health() {
		if entry.ID == "flaky" {
			h = entry
		}
	}
	if h.ID == "" {
		t.Fatal("crash-looping provider vanished from health")
	}
	if h.Restarts < 2 {
		t.Errorf("restarts = %d, want >= 2 for a crash loop", h.Restarts)
	}
	if h.LastError == "" {
		t.Error("crash-looping provider reports no LastError — health is not observable")
	}
}

// TestExecSupervisorShutdownReapsRealProcess: daemon shutdown leaves no
// orphaned provider behind.
func TestExecSupervisorShutdownReapsRealProcess(t *testing.T) {
	env := append(os.Environ(), helperEnvMode+"="+helperModeServe)
	l := NewLifecycle(LifecycleArgs{
		Launcher: &ExecLauncher{Env: env},
		Backoff:  Backoff{Base: 10 * time.Millisecond, Max: 50 * time.Millisecond, Factor: 2},
	})
	if err := l.Reconcile([]config.ProviderDecl{helperDecl(t, "helper")}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	h := waitForPhase(t, l, "helper", PhaseRunning)
	pid := h.PID

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := l.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	waitFor(t, "provider reaped by shutdown", func() bool { return !processAlive(pid) })
}

// TestExecLauncherRejectsMissingBinary: an unrunnable declaration is
// reported as failed, not retried forever.
func TestExecLauncherRejectsMissingBinary(t *testing.T) {
	l := NewLifecycle(LifecycleArgs{
		Launcher: &ExecLauncher{},
		Backoff:  Backoff{Base: time.Millisecond, Max: time.Millisecond, Factor: 1},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testPoll)
		defer cancel()
		_ = l.Shutdown(ctx)
	})

	decl := config.ProviderDecl{
		ID:        "ghost",
		Transport: config.ProviderTransportLaunch,
		Params:    map[string]any{"exec": "/nonexistent/jevons-test-provider-binary"},
	}
	if err := l.Reconcile([]config.ProviderDecl{decl}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	h := waitForPhase(t, l, "ghost", PhaseFailed)
	if !strings.Contains(h.LastError, "unrunnable") {
		t.Errorf("LastError = %q, want it to name the unrunnable declaration", h.LastError)
	}
}

// TestArgvResolution pins how exec/argv map to a command line.
func TestArgvResolution(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   []string
		errIs  bool
	}{
		{
			name:   "exec with argv",
			params: map[string]any{"exec": "/bin/prov", "argv": []any{"--serve", "--port", "9100"}},
			want:   []string{"/bin/prov", "--serve", "--port", "9100"},
		},
		{
			name:   "exec alone",
			params: map[string]any{"exec": "/bin/prov"},
			want:   []string{"/bin/prov"},
		},
		{
			name:   "bare argv takes argv[0] as the binary",
			params: map[string]any{"argv": []any{"/bin/prov", "--serve"}},
			want:   []string{"/bin/prov", "--serve"},
		},
		{
			name:   "neither is unrunnable",
			params: map[string]any{"url": "http://x"},
			errIs:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Argv(config.ProviderDecl{ID: "p", Transport: config.ProviderTransportLaunch, Params: tc.params})
			if tc.errIs {
				if err == nil {
					t.Fatal("want an unrunnable error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Argv: %v", err)
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("Argv = %v, want %v", got, tc.want)
			}
		})
	}
}

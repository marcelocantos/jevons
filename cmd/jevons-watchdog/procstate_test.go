// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestIsStoppedStat(t *testing.T) {
	cases := []struct {
		stat string
		want bool
	}{
		{"R", false},
		{"R+", false},
		{"S", false},
		{"S+", false},
		{"I", false},
		{"D", false},
		{"Z", false},
		{"T", true},
		{"T+", true},
		{"Ts", true},
		{"", false},
		{"   ", false},
	}
	for _, c := range cases {
		if got := isStoppedStat(c.stat); got != c.want {
			t.Errorf("isStoppedStat(%q) = %v, want %v", c.stat, got, c.want)
		}
	}
}

// TestProcessIsStoppedMatchesRealSIGSTOP is the 🎯T1068 hermetic oracle: a
// real child process frozen with SIGSTOP — exactly how the PO froze
// jevonsd on 2026-10-12 — is classified stopped, and a sibling that is
// merely running hard (slow, but schedulable) is not confused with it.
func TestProcessIsStoppedMatchesRealSIGSTOP(t *testing.T) {
	// "Running but slow": a CPU-bound fixture, never job-control-stopped.
	busy := exec.Command("sh", "-c", "while :; do :; done")
	if err := busy.Start(); err != nil {
		t.Fatalf("starting busy fixture: %v", err)
	}
	t.Cleanup(func() { _ = busy.Process.Kill() })

	frozen := exec.Command("sleep", "30")
	if err := frozen.Start(); err != nil {
		t.Fatalf("starting fixture to freeze: %v", err)
	}
	t.Cleanup(func() { _ = frozen.Process.Kill() })

	if err := frozen.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatalf("SIGSTOP: %v", err)
	}
	t.Cleanup(func() { _ = frozen.Process.Signal(syscall.SIGCONT) })

	// ps needs a beat to reflect the signal.
	deadline := time.Now().Add(2 * time.Second)
	var stopped bool
	var stat string
	var err error
	for time.Now().Before(deadline) {
		stopped, stat, err = processIsStopped(frozen.Process.Pid)
		if err == nil && stopped {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("processIsStopped(frozen): %v", err)
	}
	if !stopped {
		t.Errorf("frozen fixture (pid %d) reported not stopped, ps stat=%q", frozen.Process.Pid, stat)
	}

	if ok, stat, err := processIsStopped(busy.Process.Pid); err != nil {
		t.Fatalf("processIsStopped(busy): %v", err)
	} else if ok {
		t.Errorf("busy, running fixture (pid %d) reported stopped, ps stat=%q", busy.Process.Pid, stat)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildDetach compiles the real binary; the properties under test live in
// process teardown (os.Exit racing the streaming goroutine), which an
// in-process call to run() cannot exercise.
func buildDetach(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "detach")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build detach: %v\n%s", err, out)
	}
	return bin
}

// TestStreamedOutputSurvivesTheChildExiting is the 🎯T405 evidence
// regression: a caller reading a detached restart on a pipe must see the
// whole run, up to and including the last line the child wrote.
//
// The bug it locks out: the parent signalled its tail goroutine and
// returned, and os.Exit does not wait for goroutines, so the final lines
// were dropped whenever the copy lost the race. The line at risk is the
// one that matters — "OK: daily jevonsd serving" — and losing it turns a
// restart that worked into a report that says it did not.
func TestStreamedOutputSurvivesTheChildExiting(t *testing.T) {
	bin := buildDetach(t)
	logPath := filepath.Join(t.TempDir(), "run.log")

	const lines = 300
	// Written in one burst immediately before exit, which is where the
	// race lived: the child's last write and its exit are adjacent.
	child := fmt.Sprintf("i=1; while [ $i -le %d ]; do echo line-$i; i=$((i+1)); done", lines)

	cmd := exec.Command(bin, "-quiet", "-log", logPath, "--", "/bin/sh", "-c", child)
	// CombinedOutput gives the child's parent a pipe, not a regular file,
	// which is what puts the parent on the streaming path at all.
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("detach failed: %v\n%s", err, out)
	}

	got := string(out)
	if n := strings.Count(got, "line-"); n != lines {
		t.Errorf("streamed %d of %d lines back to the caller; the tail was truncated", n, lines)
	}
	if !strings.Contains(got, fmt.Sprintf("line-%d\n", lines)) {
		t.Errorf("last line missing from the caller's view:\n%s", lastBytes(got, 400))
	}

	// The log the child actually wrote is the reference: whatever is on
	// disk is what the caller should have read.
	onDisk, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read detach log: %v", readErr)
	}
	if a, b := strings.Count(string(onDisk), "line-"), strings.Count(got, "line-"); a != b {
		t.Errorf("log holds %d lines, caller saw %d", a, b)
	}
}

// TestExitStatusIsTheChilds: a caller that waits on a restart wants the
// restart's answer, not the wrapper's.
func TestExitStatusIsTheChilds(t *testing.T) {
	bin := buildDetach(t)
	logPath := filepath.Join(t.TempDir(), "run.log")

	cmd := exec.Command(bin, "-quiet", "-log", logPath, "--", "/bin/sh", "-c", "echo nope >&2; exit 7")
	out, err := cmd.CombinedOutput()
	var code int
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("detach failed oddly: %v\n%s", err, out)
	}
	if code != 7 {
		t.Errorf("detach exited %d, want the child's 7\n%s", code, out)
	}
	if !strings.Contains(string(out), "nope") {
		t.Errorf("child stderr did not reach the caller:\n%s", out)
	}
}

func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T1259: -detach returns at once and -result reports the landing's own
// outcome and exit status, so a worker whose shell call is capped at 600 s
// can land. A landing refused for a worker branch that does not exist is the
// fast end-to-end case: the outcome must reach -result, not be lost or
// overwritten by the detaching parent.
func TestT1259DetachedLandingReportsItsOutcome(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "integrate")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build integrate: %v\n%s", err, out)
	}
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "master"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "root"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	dir := t.TempDir()
	env := append(os.Environ(), "JEVONS_INTEGRATE_DIR="+dir, "JEVONS_GATE_DIR="+t.TempDir())

	detach := exec.Command(bin, "-repo", repo, "-detach", "jv-no-such-worker")
	detach.Env = env
	out, err := detach.CombinedOutput()
	if err != nil {
		t.Fatalf("detach: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "INTEGRATE detached pid=") {
		t.Fatalf("detach output does not name the detached run:\n%s", out)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		result := exec.Command(bin, "-result", "jv-no-such-worker")
		result.Env = env
		out, err := result.CombinedOutput()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("result: %v\n%s", err, out)
		}
		if code == 3 {
			if time.Now().After(deadline) {
				t.Fatalf("detached landing still running after 30s:\n%s", out)
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if code != 1 || !strings.Contains(string(out), "REFUSED exit=1") {
			t.Fatalf("result exit=%d, want 1 with REFUSED exit=1:\n%s", code, out)
		}
		if !strings.Contains(string(out), "jv-no-such-worker") {
			t.Fatalf("result does not carry the landing's own log tail:\n%s", out)
		}
		return
	}
}

// -result for an agent with no detached landing says so instead of
// inventing an outcome.
func TestT1259ResultWithoutALandingIsAnError(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "integrate")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build integrate: %v\n%s", err, out)
	}
	result := exec.Command(bin, "-result", "jv-never-landed")
	result.Env = append(os.Environ(), "JEVONS_INTEGRATE_DIR="+t.TempDir())
	out, err := result.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "no detached landing") {
		t.Fatalf("result with no landing: err=%v\n%s", err, out)
	}
}

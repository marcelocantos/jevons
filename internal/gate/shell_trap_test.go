// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"io"
	"os/exec"
	"strings"
	"testing"
)

// This file reproduces the live evidence of 2026-08-10 as executable fact,
// rather than trusting the write-up. A worker (running under zsh, as this
// harness does) gated the web suite with:
//
//	make test-web 2>&1 | tail -25; echo "EXIT=${PIPESTATUS[0]}"
//
// and got a bare `EXIT=`. Two independent defects stack in that one line:
// the pipeline hands its status to tail, and PIPESTATUS is bash's spelling,
// so under zsh the interpolation is empty and no status is read at all. The
// re-run happened to be green, so nothing was misreported — but the shape is
// 🎯T386 exactly.

func shellOutput(t *testing.T, shell, script string) string {
	t.Helper()
	path, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s not on PATH", shell)
	}
	out, err := exec.Command(path, "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%s -c %q: %v (%s)", shell, script, err, out)
	}
	return strings.TrimSpace(string(out))
}

// The trap itself: the same line means different things in the two shells,
// and the one this harness runs is the one that loses the status.
func TestT386ZshHasNoPIPESTATUS(t *testing.T) {
	const script = `false | tail -1; echo "EXIT=${PIPESTATUS[0]}"`

	if got := shellOutput(t, "zsh", script); got != "EXIT=" {
		t.Fatalf("zsh printed %q; the trap this target closes has changed shape "+
			"and the guidance in the fleet brief needs revisiting", got)
	}
	if got := shellOutput(t, "bash", script); got != "EXIT=1" {
		t.Fatalf("bash printed %q, want EXIT=1", got)
	}
}

// zsh's own array is spelled differently AND indexes from 1, so a worker who
// fixes only the name still reads nothing.
func TestT386ZshPipestatusIndexesFromOne(t *testing.T) {
	if got := shellOutput(t, "zsh", `false | tail -1; echo "EXIT=${pipestatus[0]}"`); got != "EXIT=" {
		t.Fatalf("zsh ${pipestatus[0]} printed %q, want empty", got)
	}
	if got := shellOutput(t, "zsh", `false | tail -1; echo "EXIT=${pipestatus[1]}"`); got != "EXIT=1" {
		t.Fatalf("zsh ${pipestatus[1]} printed %q, want EXIT=1", got)
	}
}

// The masking half, independent of the array spelling: even where PIPESTATUS
// works, the plain `$?` after a pipeline is the last stage's.
func TestT386PipelineStatusIsTheLastStage(t *testing.T) {
	if got := shellOutput(t, "sh", `false | tail -1; echo "EXIT=$?"`); got != "EXIT=0" {
		t.Fatalf("pipeline status = %q, want EXIT=0 — the masking this target "+
			"closes is gone and the fixture needs rewriting", got)
	}
}

// And the mechanism's answer: there is no shell and no pipeline between gate
// and the command, so the same failing gate surfaces its own status.
func TestT386GateGivesTheStatusThePipelineAte(t *testing.T) {
	rec, err := Run(&RunArgs{
		Command: []string{"false"},
		Name:    "make-test-web",
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status() != "1" || rec.Verdict != VerdictRed {
		t.Fatalf("gate reported exit=%s %s for a failing gate, want exit=1 %s",
			rec.Status(), rec.Verdict, VerdictRed)
	}
}

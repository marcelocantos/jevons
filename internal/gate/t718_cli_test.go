// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T718 through the shipped binary: a passing command in a dirty tree must
// still exit 0 (inner-loop && chains keep working) while the GATE line is
// DIRTY, not GREEN, and names -clean.

func TestT718GateCLIDirtyPassExitsZeroAndIsNotGreen(t *testing.T) {
	root := t718CLIRepo(t, true)
	store := t.TempDir()

	code, out := runGate(t, store, "-dir", root, "--", "true")
	if code != 0 {
		t.Fatalf("dirty pass exited %d, want the command's own 0\n%s", code, out)
	}
	if !strings.Contains(out, "exit=0 DIRTY") {
		t.Fatalf("GATE line missing DIRTY:\n%s", out)
	}
	if strings.Contains(out, "exit=0 GREEN") {
		t.Fatalf("mutation: dirty CLI run restored a bare GREEN:\n%s", out)
	}
	if !strings.Contains(out, "tree=dirty+") {
		t.Fatalf("GATE line missing dirty marker:\n%s", out)
	}
	if !strings.Contains(out, gate.DirtyHintToken) {
		t.Fatalf("GATE line missing %s:\n%s", gate.DirtyHintToken, out)
	}
	if !strings.Contains(out, "bin/gate -clean") {
		t.Fatalf("dirty-run message does not name bin/gate -clean:\n%s", out)
	}

	lastCode, lastOut := runGate(t, store, "last")
	if lastCode == 0 {
		t.Fatalf("gate last treated a DIRTY run as a pass:\n%s", lastOut)
	}
	if !strings.Contains(lastOut, "DIRTY") {
		t.Fatalf("gate last lost the DIRTY verdict:\n%s", lastOut)
	}
}

func TestT718GateCLICleanPassStaysGreen(t *testing.T) {
	root := t718CLIRepo(t, false)
	store := t.TempDir()

	code, out := runGate(t, store, "-dir", root, "--", "true")
	if code != 0 {
		t.Fatalf("clean pass exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "exit=0 GREEN") {
		t.Fatalf("clean GATE line missing GREEN:\n%s", out)
	}
	if strings.Contains(out, "DIRTY") {
		t.Fatalf("clean run marked DIRTY:\n%s", out)
	}
	if strings.Contains(out, "bin/gate -clean") {
		t.Fatalf("clean run named -clean:\n%s", out)
	}
}

func t718CLIRepo(t *testing.T, dirty bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", "keep.txt")
	run("-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "keep")
	if dirty {
		if err := os.WriteFile(filepath.Join(root, "foreign.txt"), []byte("wip\n"), 0o644); err != nil {
			t.Fatalf("write foreign: %v", err)
		}
	}
	return root
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sharedcloneseats_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/sharedcloneseats"
	"github.com/marcelocantos/jevons/internal/worktree"
)

func TestSharedCloneForWorktreeSeat(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "jevons")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", base}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
	wt, err := worktree.Ensure(base, "jv-seat")
	if err != nil {
		t.Fatal(err)
	}
	shared, ok := sharedcloneseats.SharedCloneForWorktreeSeat(wt)
	if !ok || filepath.Clean(shared) != filepath.Clean(base) {
		t.Fatalf("SharedCloneForWorktreeSeat(%q) = %q, %v; want %q, true", wt, shared, ok, base)
	}
	if !sharedcloneseats.PathUnderSharedClone(base, filepath.Join(base, "bullseye.yaml")) {
		t.Fatal("PathUnderSharedClone missed file under shared")
	}
}

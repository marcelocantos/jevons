// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T1151.4: a linked worktree seat must not run `git reset --hard` against
// the shared clone (04:30:50 incident shape).
func TestWorkSeatRefusesGitResetHardAgainstSharedClone(t *testing.T) {
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
	run("init", "-q", "-b", "master")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
	wt, err := worktree.Ensure(base, "jv-t1151.4-seat")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(commitattrib.ActorEnv, "jv-t1151.4-seat")
	cmd := "git -C " + base + " reset --hard"
	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: wt, Now: time.Now}
	got, err := env.Pre(bashPayload(wt, cmd))
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != Deny {
		t.Fatalf("Pre(%q) = %+v, want Deny", cmd, got)
	}
	if !strings.Contains(got.Message, "jv-t1151.4-seat") || !strings.Contains(got.Message, "reset --hard") {
		t.Fatalf("refusal must name seat and command: %q", got.Message)
	}
}

// 🎯T1151.4 hermetic: a fleet seat still in the shared clone (pre-isolation
// shape) must not run `git reset --hard` or `git stash` there.
func TestFleetSeatInSharedCloneRefusesResetHardAndStash(t *testing.T) {
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
	run("init", "-q", "-b", "master")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")
	t.Setenv(commitattrib.ActorEnv, "jv-t1151.4-legacy-seat")
	env := &Env{Store: &Store{Root: t.TempDir()}, RepoRoot: base, Now: time.Now}
	for _, cmd := range []string{"git reset --hard", "git stash"} {
		got, err := env.Pre(bashPayload(base, cmd))
		if err != nil {
			t.Fatalf("Pre(%q): %v", cmd, err)
		}
		if got.Verdict != Deny {
			t.Fatalf("Pre(%q) = %+v, want Deny", cmd, got)
		}
		if !strings.Contains(got.Message, "jv-t1151.4-legacy-seat") {
			t.Fatalf("refusal must name seat: %q", got.Message)
		}
	}
}

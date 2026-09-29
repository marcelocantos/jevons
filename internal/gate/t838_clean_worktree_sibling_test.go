// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T440 //worktreereap:exempt — both worktrees created here live inside
// t.TempDir: no shared clone ever lists them, so there is nothing for the
// 🎯T440 sweeper to reap even if this test dies before cleanup.
// 🎯T838: a fleet worker runs `bin/gate -clean` from a linked worktree under
// .jevons-worktrees-jevons/<name>. That directory's parent holds other
// workers' trees, not claudia, so the sibling go.work was never injected and
// the clean build used the published claudia pin (gate 5361afa0 RED:
// undefined claudia.SubscriptionSeatProvider). The injector must find the
// siblings beside the shared clone the worktree belongs to.

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func TestInjectCleanSiblingGoWorkFromLinkedWorktree(t *testing.T) {
	org, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(org, "jevons")
	writeGoMod(t, clone, "github.com/marcelocantos/jevons")
	gitIn(t, clone, "init", "-q", "-b", "master")
	gitIn(t, clone, "add", "go.mod")
	gitIn(t, clone, "commit", "-q", "-m", "init")
	writeGoMod(t, filepath.Join(org, "claudia"), "github.com/marcelocantos/claudia")

	root := filepath.Join(org, ".jevons-worktrees-jevons", "jv-worker")
	gitIn(t, clone, "worktree", "add", "-q", "--detach", root)

	wt := filepath.Join(org, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	restore := injectCleanSiblingGoWork(root, wt)
	defer restore()

	data, err := os.ReadFile(filepath.Join(wt, "go.work"))
	if err != nil {
		t.Fatalf("go.work not written for a linked worktree root: %v", err)
	}
	want := "replace github.com/marcelocantos/claudia => " + filepath.Join(org, "claudia")
	if !strings.Contains(string(data), want) {
		t.Fatalf("go.work does not point claudia at the shared clone's sibling; want %q in:\n%s", want, data)
	}
}

// Control: a worktree whose shared clone has no sibling beside it gets no
// go.work, so the published pin is still what an ordinary checkout builds.
func TestInjectCleanSiblingGoWorkLinkedWorktreeWithoutSibling(t *testing.T) {
	org, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(org, "jevons")
	writeGoMod(t, clone, "github.com/marcelocantos/jevons")
	gitIn(t, clone, "init", "-q", "-b", "master")
	gitIn(t, clone, "add", "go.mod")
	gitIn(t, clone, "commit", "-q", "-m", "init")
	root := filepath.Join(org, ".jevons-worktrees-jevons", "jv-worker")
	gitIn(t, clone, "worktree", "add", "-q", "--detach", root)

	wt := filepath.Join(org, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	restore := injectCleanSiblingGoWork(root, wt)
	defer restore()
	if _, err := os.Stat(filepath.Join(wt, "go.work")); !os.IsNotExist(err) {
		t.Fatalf("go.work written with no sibling anywhere: err=%v", err)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T398: the web suite must be green on the *committed* tree, not merely in
// the shared clone. Many workers share one working copy, so `make test-web`
// run there reads everyone's uncommitted edits — a file that only exists as
// someone's WIP can hold the suite green while a fresh clone, a CI runner, or
// the next worker to check master out gets red. That is exactly how master
// stayed red from 8297ae6 ("fix(T381): agent reports render as markdown") for
// long enough that the regression was found by 🎯T388's gate rather than by
// the suite that covers it.
//
// T360 ratchets the same shape for the Go build; this is its web half. The
// two are deliberately separate tests: a broken embed input and a red web
// assertion fail for unrelated reasons and should not share a failure message.
package docratchet_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// TestT398CleanCheckoutWebTestsPass checks HEAD out into a detached worktree
// in a temp dir and runs `make test-web` there. The worktree carries only
// tracked, committed content — no node_modules, no WIP — so a green result
// means the suite passes on what a fresh clone actually gets.
func TestT398CleanCheckoutWebTestsPass(t *testing.T) {
	root := gitRepo(t)
	for _, bin := range []string{"node", "make"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}

	// git refuses a non-empty target, so name a child of the temp dir.
	wt := filepath.Join(t.TempDir(), "head")
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "--detach", wt, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	// 🎯T440: stamp the owner, so a run killed before the cleanup below is
	// reaped from outside rather than left on disk for the sweeper to guess
	// about. `make test-web` is the longer of the two ratchets and therefore
	// the likelier one to be killed by a timeout.
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatalf("mark worktree owner: %v", err)
	}
	t.Cleanup(func() {
		// TempDir removal leaves .git/worktrees metadata behind; prune it.
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt).Run()
		_ = exec.Command("git", "-C", root, "worktree", "prune").Run()
	})

	web := exec.Command("make", "test-web")
	web.Dir = wt
	if out, err := web.CombinedOutput(); err != nil {
		t.Fatalf("`make test-web` is RED on a clean checkout of HEAD (%v).\n"+
			"The shared clone can be green on WIP that a fresh clone does not have —\n"+
			"resolve the assertion on its merits rather than in the working copy.\n%s",
			err, out)
	}
}

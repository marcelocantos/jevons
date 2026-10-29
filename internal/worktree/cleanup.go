// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// IsManagedFleetWorktree reports whether path is a leaf under the daemon's
// .jevons-worktrees-<repo>/ layout (🎯T254.2). Those trees are remint homes
// owned by a durable agent name; Integrate / the T440 sweeper own their
// lifetime. An overseer-improvised sibling (the 🎯T1311 specimen) is not.
func IsManagedFleetWorktree(path string) bool {
	_, ok := SharedCloneFromFleetWorktree(path)
	return ok
}

func protectedBranch(name string) bool {
	switch strings.TrimSpace(name) {
	case "", "HEAD", "master", "main":
		return true
	}
	return false
}

// ImprovisedCleanupNote is the always-on brief instruction for a worker
// sitting in a linked worktree outside .jevons-worktrees-<repo>/. The
// daemon also auto-cleans on finished-work reap (CleanupImprovised) once
// every land/clean/sibling guard holds; this text covers the case the
// worker or its parent lands (including by cherry-pick) before that sweep.
func ImprovisedCleanupNote(workdir string) string {
	workdir = filepath.Clean(strings.TrimSpace(workdir))
	branch, _ := gitOut(workdir, "symbolic-ref", "--quiet", "--short", "HEAD")
	common, _ := gitOut(workdir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	main := ""
	if common != "" {
		main = filepath.Dir(common)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Improvised worktree (🎯T1311): workdir %s is a linked git worktree outside the managed .jevons-worktrees-<repo>/ layout. ", workdir)
	b.WriteString("Once your work has landed on the shared clone's target branch (including via cherry-pick), and this tree is clean, remove this worktree and its feature branch so they do not linger. ")
	if main != "" {
		fmt.Fprintf(&b, "From the main repo %s run: git worktree remove %s", main, workdir)
	} else {
		fmt.Fprintf(&b, "From the main repo run: git worktree remove %s", workdir)
	}
	if branch != "" && !protectedBranch(branch) {
		fmt.Fprintf(&b, "; then git branch -d %s", branch)
	}
	b.WriteString(". Do not remove a dirty tree or a branch that has not landed (git cherry <target> <branch> must show no unique commits). Do not leave sibling worktrees behind.")
	return b.String()
}

func skipCleanup(reason, workdir string, extra ...any) {
	args := []any{"reason", reason, "workdir", workdir}
	args = append(args, extra...)
	slog.Info("T1311 skip improvised worktree cleanup", args...)
}

// branchLandedOn reports whether every commit on branch is already on
// target, including by cherry-pick. git cherry lists unique patch-ids with
// '+'; no '+' line means the branch has landed. Plain ancestry misses
// cherry-picks (the 2026-10-29 specimen).
func branchLandedOn(main, target, branch string) bool {
	out, _, err := gitRun(main, "cherry", target, branch)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "+") {
			return false
		}
	}
	return true
}

// CleanupImprovised removes an overseer-improvised sibling worktree and its
// feature branch after the work has landed (🎯T1311). No-op unless all hold:
//
//   - linked worktree, not a managed .jevons-worktrees-* tree
//   - sibling of the main clone (filepath.Dir(workdir) == filepath.Dir(main))
//   - clean including untracked (`git status --porcelain` empty)
//   - named feature branch, not master/main/detached
//   - landed on the main clone's checked-out branch: git cherry reports no
//     unique commits (cherry-picks count; ancestry alone does not)
//
// Never `git worktree remove --force`. Never a blind `git branch -D`:
// unique commits refuse the whole cleanup; after cherry proves equivalence,
// `-d` is tried first and `-D` only if `-d` still refuses a cherry-pick.
//
// 🎯T440 //worktreereap:exempt — this removes trees; it does not create them.
func CleanupImprovised(workdir string) error {
	workdir = filepath.Clean(strings.TrimSpace(workdir))
	if workdir == "" || workdir == "." {
		return nil
	}
	if IsManagedFleetWorktree(workdir) {
		skipCleanup("managed_fleet_worktree", workdir)
		return nil
	}
	if !IsGitRepo(workdir) {
		return nil
	}
	if !IsLinkedWorktree(workdir) {
		skipCleanup("not_linked_worktree", workdir)
		return nil
	}
	top, err := gitOut(workdir, "rev-parse", "--show-toplevel")
	if err != nil {
		skipCleanup("no_toplevel", workdir, "err", err)
		return nil
	}
	workdir = filepath.Clean(top)
	if IsManagedFleetWorktree(workdir) {
		skipCleanup("managed_fleet_worktree", workdir)
		return nil
	}
	if _, err := os.Stat(workdir); os.IsNotExist(err) {
		return nil
	}

	gitMu.Lock()
	defer gitMu.Unlock()

	branch, err := gitOut(workdir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || protectedBranch(branch) {
		skipCleanup("protected_or_detached_branch", workdir, "branch", branch)
		return nil
	}
	common, err := gitOut(workdir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		skipCleanup("no_git_common_dir", workdir, "err", err)
		return nil
	}
	main := filepath.Dir(common)
	if main == "" || main == workdir {
		skipCleanup("not_distinct_from_main", workdir)
		return nil
	}
	if filepath.Clean(filepath.Dir(workdir)) != filepath.Clean(filepath.Dir(main)) {
		skipCleanup("not_sibling_of_main_clone", workdir, "main", main)
		return nil
	}
	dirty, err := gitOut(workdir, "status", "--porcelain")
	if err != nil {
		skipCleanup("status_failed", workdir, "err", err)
		return nil
	}
	if dirty != "" {
		skipCleanup("dirty_tree", workdir)
		return nil
	}
	target, err := gitOut(main, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(target) == "" {
		skipCleanup("main_not_on_a_branch", workdir, "main", main)
		return nil
	}
	if !branchLandedOn(main, target, branch) {
		skipCleanup("branch_not_landed", workdir, "branch", branch, "target", target)
		return nil
	}

	out, _, err := gitRun(main, "worktree", "remove", workdir)
	if err != nil {
		skipCleanup("worktree_remove_refused", workdir, "err", err, "out", out)
		return nil
	}
	if _, st := os.Stat(workdir); !os.IsNotExist(st) {
		skipCleanup("worktree_still_on_disk", workdir)
		return nil
	}
	if out, _, err := gitRun(main, "branch", "-d", branch); err != nil {
		// -d refuses cherry-picks (different SHAs). Cherry already proved
		// patch-id equivalence, so -D is not blind.
		if out2, _, err2 := gitRun(main, "branch", "-D", branch); err2 != nil {
			slog.Info("T1311 worktree removed but branch delete refused",
				"branch", branch, "d", err, "d_out", out, "D", err2, "D_out", out2)
		}
	}
	return nil
}

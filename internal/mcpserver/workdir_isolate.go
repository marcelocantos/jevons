// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// isolateFleetWorkSeat redirects a fleet-managed work agent into its own git
// worktree when 🎯T1151.1 requires it. base is the spawn path's nominal repo
// root (expected: shared clone). Returned workdir is what register/launch must
// use; isolationNote is non-empty when a new tree was ensured (for brief prepend)
// or when the seat is already in an improvised linked worktree (🎯T1311 cleanup
// instruction).
func isolateFleetWorkSeat(base, name, purpose, role string) (workdir, isolationNote string, life map[string]any) {
	life = map[string]any{}
	workdir = base
	if !worktree.NeedsIsolation(purpose, role) {
		return workdir, "", life
	}
	shared := base
	if clone, ok := worktree.SharedCloneFromFleetWorktree(base); ok {
		shared = clone
	} else if worktree.IsLinkedWorktree(base) {
		// A linked tree outside the fleet worktrees layout is already isolated
		// from the shared clone; nesting another worker tree is refused in Ensure.
		// 🎯T1311: the 2026-10-29 isolate left sibling trees (wordfreq-json /
		// feature/json) behind after cherry-pick landing. Instruct the worker
		// to self-clean, and stamp life so reap can auto-remove the tree
		// once every land/clean/sibling guard holds.
		life["improvised_worktree"] = true
		life["workdir"] = base
		return workdir, worktree.ImprovisedCleanupNote(base), life
	}
	if !worktree.IsGitRepo(shared) {
		return workdir, "", life
	}
	want := worktree.WorktreePath(shared, name)
	if filepath.Clean(base) == filepath.Clean(want) {
		workdir = want
		life["workdir"] = want
		return workdir, "", life
	}
	if worktree.IsLinkedWorktree(base) {
		life["worktree_isolate_redirect"] = fmt.Sprintf("%s -> %s", base, want)
	}
	wt, err := worktree.Ensure(shared, name)
	if err != nil {
		life["worktree_isolate_err"] = err.Error()
		return workdir, "", life
	}
	life["base_workdir"] = shared
	life["workdir"] = wt
	life["worktree_isolated"] = true
	if _, err := worktree.MirrorGoWork(shared, wt); err != nil {
		life["worktree_gowork_err"] = err.Error()
	}
	workdir = wt
	isolationNote = fmt.Sprintf(
		"Worktree isolation (🎯T1151.1): workdir %s is your own git worktree on branch %s, made from the shared clone %s. "+
			"Commit there; do not commit in, merge into, or check out the shared clone. "+
			"Your commits land through the integrator: go run ./cmd/integrate -repo %s %s.",
		wt, worktree.BranchName(name), shared, shared, name)
	return workdir, isolationNote, life
}

func enforceFleetSeatWorkdir(name, purpose, role, workdir string) error {
	if !worktree.NeedsIsolation(purpose, role) {
		return nil
	}
	if err := worktree.ValidateSeatFleetWorkdir(name, workdir); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func prependIsolationBrief(prompt, isolationNote string) string {
	if isolationNote == "" || strings.TrimSpace(prompt) == "" {
		return prompt
	}
	return isolationNote + "\n\n" + prompt
}

// cleanupImprovisedAfterReap removes an overseer-improvised sibling worktree
// and its feature branch after a successful finished-work reap (🎯T1311),
// but only when CleanupImprovised's land/clean/sibling guards all hold and
// no remaining registered agent still uses that workdir.
func cleanupImprovisedAfterReap(reg *claudia.Registry, workdir string) {
	workdir = filepath.Clean(strings.TrimSpace(workdir))
	if workdir == "" || workdir == "." {
		return
	}
	if reg != nil {
		for _, d := range reg.List() {
			if filepath.Clean(strings.TrimSpace(d.WorkDir)) == workdir {
				slog.Info("T1311 skip improvised worktree cleanup",
					"reason", "workdir_still_used", "workdir", workdir, "agent", d.Name)
				return
			}
		}
	}
	if err := worktree.CleanupImprovised(workdir); err != nil {
		slog.Warn("T1311 improvised worktree cleanup failed", "workdir", workdir, "err", err)
	}
}

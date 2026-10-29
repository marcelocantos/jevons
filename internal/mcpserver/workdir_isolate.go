// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// isolateFleetWorkSeat redirects a fleet-managed work agent into its own git
// worktree when 🎯T1151.1 requires it. base is the spawn path's nominal repo
// root (expected: shared clone). Returned workdir is what register/launch must
// use; isolationNote is non-empty when a new tree was ensured (for brief prepend).
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
		return workdir, "", life
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

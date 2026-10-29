// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/sharedcloneseats"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// RefuseBranchCheckoutInSharedClone is the fleetgit PATH-wrapper entry for
// 🎯T955 / 🎯T962 (OMP seats that never read .claude hooks). checked is false
// when the guard has nothing to say.
func RefuseBranchCheckoutInSharedClone(seatWorkdir, gitCwd, command string, fleetSeat bool) (Decision, bool) {
	if !fleetSeat {
		return Decision{}, false
	}
	ref, ok, definite := DetectBranchCheckout(command)
	if !ok {
		return Decision{}, false
	}
	gitCwd = strings.TrimSpace(gitCwd)
	if gitCwd == "" {
		gitCwd = seatWorkdir
	}
	gitCwd = filepath.Clean(gitCwd)
	shared, seatOK := sharedCloneRootForFleetSeat(seatWorkdir)
	if !seatOK {
		return Decision{}, false
	}
	sharedRoot := sharedcloneseats.CanonicalGitRoot(shared)
	gitRoot := sharedcloneseats.CanonicalGitRoot(gitCwd)
	if !sharedcloneseats.PathUnderSharedClone(sharedRoot, gitRoot) {
		return Decision{}, false
	}
	if worktree.IsLinkedWorktree(gitRoot) {
		return Decision{}, false
	}
	branch, err := gitSymbolicRefShort(gitRoot)
	if err != nil || branch == "" {
		return Decision{}, false
	}
	if ref == branch {
		return Decision{}, false
	}
	if !definite && !isRefLike(gitRoot, ref) {
		return Decision{}, false
	}
	return DecideBranchCheckout(&BranchGuardArgs{CurrentBranch: branch, TargetRef: ref}), true
}

func sharedCloneRootForFleetSeat(seatWorkdir string) (shared string, ok bool) {
	if shared, ok := sharedcloneseats.SharedCloneForWorktreeSeat(seatWorkdir); ok {
		return shared, true
	}
	seatWorkdir = filepath.Clean(seatWorkdir)
	if seatWorkdir == "" || worktree.IsLinkedWorktree(seatWorkdir) {
		return "", false
	}
	return seatWorkdir, true
}

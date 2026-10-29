// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package sharedcloneseats maps fleet work-tree seats back to their shared
// clone and refuses writes into that clone (🎯T1151.1). It lives below
// internal/worktree so fleetgit and treeguard can share the logic without
// pulling worktree's ledgercommit dependency (import-cycle cut).
package sharedcloneseats

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SharedCloneForWorktreeSeat maps a fleet worker's linked worktree back to
// the shared clone it was made from (🎯T1151.1). ok is false when seatDir is
// not under a .jevons-worktrees-<repo>/ tree.
func SharedCloneForWorktreeSeat(seatDir string) (shared string, ok bool) {
	seatDir = filepath.Clean(seatDir)
	parent := filepath.Dir(seatDir)
	dirName := filepath.Base(parent)
	const prefix = ".jevons-worktrees-"
	if !strings.HasPrefix(dirName, prefix) {
		return "", false
	}
	repoName := strings.TrimPrefix(dirName, prefix)
	if repoName == "" {
		return "", false
	}
	base := filepath.Join(filepath.Dir(parent), repoName)
	if st, err := os.Stat(base); err != nil || !st.IsDir() {
		return "", false
	}
	if !sharesRepo(base, seatDir) {
		return "", false
	}
	if isLinkedWorktree(base) {
		return "", false
	}
	return base, true
}

// PathUnderSharedClone reports whether abs names a path inside the shared
// clone's working tree (including the root directory itself).
func PathUnderSharedClone(sharedRoot, abs string) bool {
	sharedRoot = filepath.Clean(sharedRoot)
	abs = filepath.Clean(abs)
	if sharedRoot == "" || abs == "" {
		return false
	}
	if abs == sharedRoot {
		return true
	}
	rel, err := filepath.Rel(sharedRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// RefuseSharedCloneGitCommandMessage names the fleet seat and git command for a
// denial when a work-tree seat tries to move refs or rewrite the shared clone
// (🎯T1151.4).
func RefuseSharedCloneGitCommandMessage(seatName, sharedRoot, gitCommand string) string {
	seatName = strings.TrimSpace(seatName)
	if seatName == "" {
		seatName = "fleet seat"
	}
	gitCommand = strings.TrimSpace(gitCommand)
	if gitCommand == "" {
		gitCommand = "git"
	}
	return fmt.Sprintf(
		"treeguard: seat %q refused %q in shared checkout %s — "+
			"only `go run ./cmd/integrate` and the ledger writer may move the "+
			"shared branch; commit in your own worktree (🎯T1151.4 / 🎯T1160). "+
			"When integrate refuses a landing, report blocked to your parent PO — "+
			"never hand-commit here as a fallback.",
		seatName, gitCommand, sharedRoot,
	)
}


// GitTopLevel returns the absolute path of the git working tree rooted at dir.
func GitTopLevel(dir string) (string, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", false
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel", "--path-format=absolute").Output()
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	return filepath.Clean(root), true
}

// CanonicalGitRoot returns the git working tree root for dir when it
// lies inside a repository, otherwise filepath.Clean(dir). Use this before
// comparing paths from filesystem layout with paths from git (macOS /var vs
// /private/var).
func CanonicalGitRoot(dir string) string {
	if root, ok := GitTopLevel(dir); ok {
		return root
	}
	return filepath.Clean(dir)
}

// IsSharedCloneRoot reports whether dir is the main checkout of a repository
// (not a linked worktree).
func IsSharedCloneRoot(dir string) bool {
	root, ok := GitTopLevel(dir)
	if !ok {
		return false
	}
	return !isLinkedWorktree(root)
}

// RefuseForeignSharedCloneWriteMessage names the sibling repo's product owner
// and the jevons_agent_send route (🎯T1151.6).
func RefuseForeignSharedCloneWriteMessage(seatName, foreignRepo, foreignRoot string) string {
	seatName = strings.TrimSpace(seatName)
	if seatName == "" {
		seatName = "fleet seat"
	}
	foreignRepo = strings.TrimSpace(foreignRepo)
	if foreignRepo == "" {
		foreignRepo = "sibling"
	}
	po := foreignRepo + "-po"
	return fmt.Sprintf(
		"fleetgit: seat %q refused mutating git against %s's shared checkout %s (🎯T1151.6).\n"+
			"Fleet work agents commit only in their own worktree; sibling-repo changes go through %s via "+
			"jevons_agent_send(name=\"%s\", message=...) with a needs-release request — not a commit in that clone.\n",
		seatName, foreignRepo, foreignRoot, po, po,
	)
}

// RefuseForeignSharedCloneCommandWriteMessage names the sibling PO, worktree
// isolation, and private build paths (🎯T1151.8).
func RefuseForeignSharedCloneCommandWriteMessage(seatName, foreignRepo, foreignRoot, verb string) string {
	seatName = strings.TrimSpace(seatName)
	if seatName == "" {
		seatName = "fleet seat"
	}
	foreignRepo = strings.TrimSpace(foreignRepo)
	if foreignRepo == "" {
		foreignRepo = "sibling"
	}
	verb = strings.TrimSpace(verb)
	if verb == "" {
		verb = "write"
	}
	po := foreignRepo + "-po"
	return fmt.Sprintf(
		"treeguard: seat %q refused %s against %s's shared checkout %s (🎯T1151.8).\n"+
			"Fleet work agents build and write only inside their own worktree; use -o under /tmp or a private path, "+
			"or an isolate broker with its own -state-dir for live probes — never into a sibling's shared clone.\n"+
			"Unpublished sibling changes go through %s via jevons_agent_send(name=\"%s\", message=...) with a needs-release request.\n",
		seatName, verb, foreignRepo, foreignRoot, po, po,
	)
}

// RefuseSharedCloneWriteMessage names the fleet seat and path for a denial
// when a work-tree seat tries to mutate the shared checkout (🎯T1151.1).
func RefuseSharedCloneWriteMessage(seatName, sharedRoot, target string) string {
	seatName = strings.TrimSpace(seatName)
	if seatName == "" {
		seatName = "fleet seat"
	}
	return fmt.Sprintf(
		"treeguard: seat %q refused write to shared checkout %s (target %s) — "+
			"fleet work agents commit only inside their own worktree; land with "+
			"`go run ./cmd/integrate` (🎯T1151.1).",
		seatName, sharedRoot, target,
	)
}

func worktreesDir(baseWorkdir string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(baseWorkdir)),
		".jevons-worktrees-"+filepath.Base(filepath.Clean(baseWorkdir)))
}

func sharesRepo(baseWorkdir, otherWorkdir string) bool {
	if strings.TrimSpace(baseWorkdir) == "" || strings.TrimSpace(otherWorkdir) == "" {
		return false
	}
	base, other := filepath.Clean(baseWorkdir), filepath.Clean(otherWorkdir)
	return other == base || strings.HasPrefix(other, worktreesDir(base)+string(filepath.Separator))
}

func isLinkedWorktree(dir string) bool {
	gd, err1 := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-dir").Output()
	cd, err2 := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.TrimSpace(string(gd)) != strings.TrimSpace(string(cd))
}

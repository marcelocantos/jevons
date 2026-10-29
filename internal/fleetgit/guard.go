// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package fleetgit guards git verbs that must not run in the shared checkout
// from a fleet seat (🎯T1134). treeguard's PreToolUse hook and cmd/fleetgit
// (a PATH wrapper) share this logic.
package fleetgit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/sharedcloneseats"
)

// MaxNamedDirty caps how many dirty paths a refusal lists.
const MaxNamedDirty = 20

// RefuseStashPush reports whether a fleet seat in the shared clone must not
// run any mutating `git stash` form while tracked paths are dirty (push/save/
// pop/apply/bare). ok is
// false when this guard has nothing to say (not a fleet seat, linked
// worktree, or not a git repo).
func RefuseStashPush(repoRoot string, fleetSeat bool) (refuse bool, message string, ok bool) {
	if !fleetSeat {
		return false, "", false
	}
	if isLinkedWorktree(repoRoot) {
		return false, "", false
	}
	paths, err := DirtyTrackedPaths(repoRoot)
	if err != nil {
		return false, "", false
	}
	if len(paths) == 0 {
		return false, "", true
	}
	return true, stashRefusal(paths), true
}

// FleetSeat reports whether the process is a registered fleet worker seat.
func FleetSeat() bool {
	return strings.TrimSpace(os.Getenv(commitattrib.ActorEnv)) != ""
}

// DirtyTrackedPaths lists repo-relative paths with uncommitted tracked
// changes (same set `git stash push` would take by default).
func DirtyTrackedPaths(repoRoot string) ([]string, error) {
	out, err := exec.Command("git", "-C", repoRoot, "status", "--porcelain", "--untracked-files=no").Output()
	if err != nil {
		return nil, err
	}
	var paths []string
	// Do not TrimSpace the whole blob — porcelain lines begin with XY status
	// columns and a leading space is load-bearing (e.g. " M path").
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || len(line) < 4 {
			continue
		}
		p := strings.TrimSpace(line[3:])
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func isLinkedWorktree(dir string) bool {
	gd, err1 := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-dir").Output()
	cd, err2 := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.TrimSpace(string(gd)) != strings.TrimSpace(string(cd))
}

func stashRefusal(paths []string) string {
	var b strings.Builder
	b.WriteString("fleetgit: refusing `git stash` in the shared checkout (🎯T1134).\n\n")
	b.WriteString("Stashing would take another actor's uncommitted work. The landing path is\n")
	b.WriteString("`go run ./cmd/integrate` — it never stashes. Dirty paths that would be stashed:\n")
	for i, p := range paths {
		if i == MaxNamedDirty {
			fmt.Fprintf(&b, "  … and %d more\n", len(paths)-MaxNamedDirty)
			break
		}
		fmt.Fprintf(&b, "  %s\n", p)
	}
	b.WriteString("\nCommit your own work in your isolated worktree, land with integrate, or wait\n")
	b.WriteString("for the other actor to commit. Never stash, checkout --, restore, or reset\n")
	b.WriteString("the shared checkout.\n")
	return b.String()
}

// RefuseSharedCloneGitWrite reports whether a fleet work seat in its own
// linked worktree must not run a git write verb against the shared clone
// (🎯T1151.1 / 🎯T1151.4). seatWorkdir is the seat's project dir; gitCwd is
// the directory passed to git (-C or implicit cwd). gitCommand is quoted in
// the refusal (e.g. `git -C … reset --hard`). ok is false when N/A.
func RefuseSharedCloneGitWrite(seatWorkdir, gitCwd string, fleetSeat bool, gitCommand string) (refuse bool, message string, ok bool) {
	if !fleetSeat {
		return false, "", false
	}
	gitCwd = strings.TrimSpace(gitCwd)
	if gitCwd == "" {
		gitCwd = seatWorkdir
	}
	gitCwd = filepath.Clean(gitCwd)
	seatName := strings.TrimSpace(os.Getenv(commitattrib.ActorEnv))

	if refuse, msg, ok := refuseForeignSharedCloneGitWrite(seatWorkdir, gitCwd, seatName); ok {
		return refuse, msg, ok
	}

	shared, seatOK := sharedCloneRootForSeat(seatWorkdir)
	if !seatOK {
		return false, "", false
	}
	sharedRoot := sharedcloneseats.CanonicalGitRoot(shared)
	gitRoot := sharedcloneseats.CanonicalGitRoot(gitCwd)
	if !sharedcloneseats.PathUnderSharedClone(sharedRoot, gitRoot) {
		return false, "", false
	}
	return true, sharedcloneseats.RefuseSharedCloneGitCommandMessage(seatName, sharedRoot, gitCommand), true
}

// refuseForeignSharedCloneGitWrite blocks a fleet work seat in repo A from
// running a git write verb against repo B's shared clone (🎯T1151.6).
func refuseForeignSharedCloneGitWrite(seatWorkdir, gitCwd, seatName string) (refuse bool, message string, ok bool) {
	targetRoot, ok := foreignSharedCloneTarget(seatWorkdir, gitCwd)
	if !ok {
		return false, "", false
	}
	repo := filepath.Base(targetRoot)
	return true, sharedcloneseats.RefuseForeignSharedCloneWriteMessage(seatName, repo, targetRoot), true
}

// RefuseForeignSharedCloneAbsWrite blocks a fleet seat from writing at absPath
// when that path lies in a sibling repository's shared clone (🎯T1151.8).
func RefuseForeignSharedCloneAbsWrite(seatWorkdir, absPath, seatName, verb string) (refuse bool, message string, ok bool) {
	targetRoot, ok := foreignSharedCloneTarget(seatWorkdir, absPath)
	if !ok {
		return false, "", false
	}
	repo := filepath.Base(targetRoot)
	return true, sharedcloneseats.RefuseForeignSharedCloneCommandWriteMessage(seatName, repo, targetRoot, verb), true
}

func foreignSharedCloneTarget(seatWorkdir, targetLoc string) (targetRoot string, refuse bool) {
	homeShared, homeOK := sharedCloneRootForSeat(seatWorkdir)
	if !homeOK {
		return "", false
	}
	targetLoc = filepath.Clean(strings.TrimSpace(targetLoc))
	if targetLoc == "" {
		return "", false
	}
	targetRoot, targetOK := gitTopLevelForPath(targetLoc)
	if !targetOK {
		return "", false
	}
	seatRoot, seatOK := sharedcloneseats.GitTopLevel(seatWorkdir)
	if seatOK && targetRoot == seatRoot {
		return "", false
	}
	if !sharedcloneseats.IsSharedCloneRoot(targetRoot) {
		return "", false
	}
	homeShared = sharedcloneseats.CanonicalGitRoot(homeShared)
	targetRoot = sharedcloneseats.CanonicalGitRoot(targetRoot)
	if targetRoot == homeShared {
		return "", false
	}
	return targetRoot, true
}

func gitTopLevelForPath(loc string) (string, bool) {
	loc = filepath.Clean(strings.TrimSpace(loc))
	if loc == "" {
		return "", false
	}
	if sharedcloneseats.IsSharedCloneRoot(loc) {
		return loc, true
	}
	dir := loc
	for i := 0; i < 32; i++ {
		if root, ok := sharedcloneseats.GitTopLevel(dir); ok {
			return root, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// RefuseSharedCloneGitStash is the combined 🎯T1134 + 🎯T1151.4 stash guard for
// the shared checkout. Dirty tracked paths get the T1134 refusal that names them;
// a clean shared tree still refuses any mutating stash form with seat + command.
func RefuseSharedCloneGitStash(seatWorkdir, gitCwd string, fleetSeat bool, gitCommand string) (refuse bool, message string, ok bool) {
	shared, seatOK := sharedCloneRootForSeat(seatWorkdir)
	if !fleetSeat || !seatOK {
		return false, "", false
	}
	gitCwd = strings.TrimSpace(gitCwd)
	if gitCwd == "" {
		gitCwd = seatWorkdir
	}
	gitCwd = filepath.Clean(gitCwd)
	sharedRoot := sharedcloneseats.CanonicalGitRoot(shared)
	gitRoot := sharedcloneseats.CanonicalGitRoot(gitCwd)
	if !sharedcloneseats.PathUnderSharedClone(sharedRoot, gitRoot) {
		return false, "", false
	}
	if refuse, msg, ok := RefuseStashPush(sharedRoot, fleetSeat); ok && refuse {
		return true, msg, true
	}
	seatName := strings.TrimSpace(os.Getenv(commitattrib.ActorEnv))
	return true, sharedcloneseats.RefuseSharedCloneGitCommandMessage(seatName, sharedRoot, gitCommand), true
}

// sharedCloneRootForSeat maps a fleet seat's project dir to the shared clone
// it must not mutate: a linked worktree under .jevons-worktrees-*, or the main
// checkout when the seat still runs directly in the shared tree (🎯T1151.4).
func sharedCloneRootForSeat(seatWorkdir string) (shared string, ok bool) {
	if shared, ok := sharedcloneseats.SharedCloneForWorktreeSeat(seatWorkdir); ok {
		return shared, true
	}
	seatWorkdir = filepath.Clean(seatWorkdir)
	if seatWorkdir == "" || isLinkedWorktree(seatWorkdir) {
		return "", false
	}
	return seatWorkdir, true
}

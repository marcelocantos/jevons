// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package worktree implements 🎯T254.2: same-repo multi-worker fan-out
// isolation. When a product owner spawns more than one worker against the
// same repo workdir, the default is one git worktree per worker rather than
// N workers racing uncommitted changes in a single shared tree. Only an
// integrator (role=boss in this codebase's role catalog) lands to the shared
// local master; a plain worker gets a private worktree checked out from HEAD
// of the base repo.
package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// gitMu serializes `git worktree add` invocations against the same repo:
// git itself is not safe for concurrent writes to .git/config, so two
// workers minted in the same daemon turn must not race the git CLI even
// though their resulting worktrees are isolated from each other.
var gitMu sync.Mutex

// sanitizeRe strips everything but the characters a filesystem path segment
// and a git branch name both tolerate, so an agent name with dots (🎯T197,
// e.g. jv-t254.2-fanout) survives unmangled.
var sanitizeRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Sanitize maps an agent name to a filesystem/branch-safe segment. Pure and
// deterministic: the same name always yields the same segment.
func Sanitize(agentName string) string {
	s := sanitizeRe.ReplaceAllString(strings.TrimSpace(agentName), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "agent"
	}
	return s
}

// WorktreesDir is the directory a base repo's per-worker worktrees live
// under. It is deliberately inside the repo's parent, not inside the repo
// itself, so it never becomes part of that repo's own tracked tree or a
// nested-worktree-of-a-worktree.
func WorktreesDir(baseWorkdir string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(baseWorkdir)),
		".jevons-worktrees-"+filepath.Base(filepath.Clean(baseWorkdir)))
}

// WorktreePath is the pure computation of one worker's isolated workdir given
// the shared base repo workdir and its own agent name. Two different agent
// names always yield two different paths (🎯T254.2 acceptance: "hermetic
// test proves concurrent workers cannot overwrite each other's trees").
func WorktreePath(baseWorkdir, agentName string) string {
	return filepath.Join(WorktreesDir(baseWorkdir), Sanitize(agentName))
}

// BranchName is the branch a worker's isolated tree is checked out on, and
// the branch Integrate lands.
func BranchName(agentName string) string {
	return "jevons-worktree/" + Sanitize(agentName)
}

// Existing reports the worker's isolated tree when one is already on disk.
// A re-minted worker goes back into it whatever its siblings are doing now:
// sending it to the shared clone instead would strand every commit it made
// on its branch where nobody is looking.
func Existing(baseWorkdir, agentName string) (string, bool) {
	path := WorktreePath(baseWorkdir, agentName)
	st, err := os.Stat(path)
	return path, err == nil && st.IsDir()
}

// SharesRepo reports whether otherWorkdir is the same repo as baseWorkdir
// for isolation purposes: the shared clone itself, or one of the isolated
// trees made from it. An isolated sibling counts, so the third worker of a
// fan-out is isolated even after the first one has finished and left the
// shared clone empty of workers. Paths are compared cleaned; a symlinked
// spelling of the same directory is not recognised.
func SharesRepo(baseWorkdir, otherWorkdir string) bool {
	if strings.TrimSpace(baseWorkdir) == "" || strings.TrimSpace(otherWorkdir) == "" {
		return false
	}
	base, other := filepath.Clean(baseWorkdir), filepath.Clean(otherWorkdir)
	return other == base || strings.HasPrefix(other, WorktreesDir(base)+string(filepath.Separator))
}

// NeedsIsolation is the pure classifier for whether a spawn should be
// redirected into its own worktree rather than reusing the shared workdir
// verbatim: purpose=work, a non-integrator role, and at least one other live
// work agent already sitting on the identical workdir. An integrator (role
// boss) and product owners / auditors / asides / overseers are never
// redirected — they are the one seat allowed to touch the shared tree
// directly and land to local master.
func NeedsIsolation(purpose, role string, sharedWithLiveWorker bool) bool {
	if strings.TrimSpace(purpose) != "work" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "boss", "product-owner", "auditor", "overseer", "aside":
		return false
	}
	return sharedWithLiveWorker
}

// IsGitRepo reports whether dir is inside a git working tree.
func IsGitRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// Ensure creates (idempotently) a git worktree for agentName off baseWorkdir
// HEAD and returns its path. baseWorkdir must be a git repo; if the worktree
// already exists (e.g. a re-briefed worker resuming) it is reused as-is
// rather than recreated. A dedicated branch per worker (named after the
// worktree path's leaf) keeps `git worktree add` from refusing a branch
// that's already checked out elsewhere.
//
// 🎯T440 //worktreereap:exempt — a worker's tree is not a verification worktree. It is
// owned by a durable agent name that outlives any one pid (a re-minted worker
// goes back into it), it sits on its own branch, which the 🎯T440 sweeper
// holds rather than reaps, and it may carry the worker's uncommitted slice.
// A pid marker would record an owner that is dead by design after the first
// idle stop.
func Ensure(baseWorkdir, agentName string) (string, error) {
	if !IsGitRepo(baseWorkdir) {
		return "", fmt.Errorf("worktree: %q is not a git repo", baseWorkdir)
	}
	// A worker started in somebody's isolated tree is already isolated from
	// the shared clone; a tree made from that tree would only nest one more
	// branch that Integrate cannot find.
	if IsLinkedWorktree(baseWorkdir) {
		return "", fmt.Errorf("worktree: %q: %w", baseWorkdir, ErrBaseIsWorktree)
	}
	path := WorktreePath(baseWorkdir, agentName)
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("worktree: mkdir parent: %w", err)
	}
	branch := BranchName(agentName)
	gitMu.Lock()
	defer gitMu.Unlock()
	// Re-check under the lock: another goroutine may have just created it.
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return path, nil
	}
	// -B: create or reset the branch at HEAD, so a stale prior branch from a
	// killed-and-reminted worker of the same name does not refuse the add.
	cmd := exec.Command("git", "-C", baseWorkdir, "worktree", "add", "-B", branch, path, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("worktree: git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return path, nil
}

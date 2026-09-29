// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// 🎯T254.2: the single integrator path. A worker isolated by Ensure commits
// on its own branch in its own tree; nothing it does there can reach the
// shared clone. Integrate is the one way those commits land on the branch the
// shared clone has checked out (local master, or whatever the owner has it
// on), and it lands them without ever leaving the shared tree half-done:
//
//  1. The merge is computed without a working tree (git merge-tree), so a
//     conflict is found before anything is touched and is reported, not
//     left in the shared clone as conflict markers.
//  2. The shared branch is advanced by a fast-forward to that result. A
//     fast-forward is a compare-and-swap on the branch tip: if someone else
//     landed in between, it is not a fast-forward any more, git refuses, and
//     Integrate recomputes from the new tip.
//     Landings are also serialized against each other by a lock in the
//     repo's git dir: git updates the index and working tree before it
//     swaps the ref, so two fast-forwards racing each other leave the loser's
//     tree rewritten under a HEAD it never reached (index says a landed file
//     is deleted, the file sits on disk untracked). The swap is the guard
//     against anything else moving the branch; the lock is the guard against
//     another integrator.
//  3. The fast-forward updates only the paths the landing changes. Other
//     workers' uncommitted edits and staged hunks in the shared clone stay
//     exactly as they were; if one of them overlaps a landed path, git
//     refuses the whole move and nothing changes.
//
// The worker's own commits are kept (a merge commit or a fast-forward, never
// a rebase), so a SHA the worker cited in its finish report stays reachable
// from the shared branch (🎯T427).

// IntegrateArgs names what to land and where.
type IntegrateArgs struct {
	// BaseWorkdir is the shared clone: the main working tree that Ensure
	// made the worker's tree from.
	BaseWorkdir string
	// AgentName is the worker whose branch lands.
	AgentName string
	// MaxAttempts bounds the recompute-and-retry loop when the shared
	// branch moves under a landing. Zero means DefaultIntegrateAttempts.
	MaxAttempts int
}

// DefaultIntegrateAttempts is how many times Integrate recomputes after
// losing the race to another landing before it gives up and says so.
const DefaultIntegrateAttempts = 5

// lockBackoff is how long a round that found the shared index locked waits
// before recomputing. Without it the retries are spent in the milliseconds
// the other landing (or a worker's `git commit`) holds the lock.
const lockBackoff = 200 * time.Millisecond

// IntegrateResult is what landed.
type IntegrateResult struct {
	BaseBranch   string
	WorkerBranch string
	// From and To are the shared branch tip before and after. Equal when
	// there was nothing to land.
	From, To string
	// Landed are the worker commits that became reachable from BaseBranch,
	// newest first. Empty when there was nothing to land.
	Landed []string
	// Merge is the merge commit Integrate created, empty for a fast-forward.
	Merge string
	// Attempts is how many compute-and-land rounds it took.
	Attempts int
}

// Refusals a caller can branch on. Each is wrapped with the detail (paths,
// git's own words) that tells the integrator what to do next.
var (
	ErrWorkerDirty    = errors.New("worker tree has uncommitted tracked changes")
	ErrConflict       = errors.New("worker branch conflicts with the shared branch")
	ErrBaseOverlap    = errors.New("shared clone has uncommitted changes to paths the landing touches")
	ErrBaseDetached   = errors.New("shared clone is not on a branch")
	ErrNotIsolated    = errors.New("no isolated worktree for this worker")
	ErrLostRace       = errors.New("shared branch kept moving")
	ErrBaseIsWorktree = errors.New("base is itself a linked worktree, not the shared clone")
)

// Integrate lands AgentName's worktree branch on the branch BaseWorkdir has
// checked out. See the package comment above for the guarantees.
func Integrate(args *IntegrateArgs) (*IntegrateResult, error) {
	base := args.BaseWorkdir
	attempts := args.MaxAttempts
	if attempts <= 0 {
		attempts = DefaultIntegrateAttempts
	}
	if !IsGitRepo(base) {
		return nil, fmt.Errorf("integrate: %q is not a git repo", base)
	}
	if IsLinkedWorktree(base) {
		return nil, fmt.Errorf("integrate: %q: %w", base, ErrBaseIsWorktree)
	}
	baseBranch, err := gitOut(base, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("integrate: %q: %w", base, ErrBaseDetached)
	}

	path := WorktreePath(base, args.AgentName)
	workerBranch := BranchName(args.AgentName)
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("integrate: %s (expected %s): %w", args.AgentName, path, ErrNotIsolated)
	}
	if got, err := gitOut(path, "symbolic-ref", "--quiet", "--short", "HEAD"); err != nil || got != workerBranch {
		return nil, fmt.Errorf("integrate: %s is on %q, not its own branch %s: %w", path, got, workerBranch, ErrNotIsolated)
	}
	// Uncommitted work would silently stay behind while the report says the
	// worker's slice landed. Untracked files are left out on purpose: they are
	// build output and scratch far more often than forgotten work.
	if dirty, err := gitOut(path, "status", "--porcelain", "--untracked-files=no"); err != nil {
		return nil, fmt.Errorf("integrate: status %s: %w", path, err)
	} else if dirty != "" {
		return nil, fmt.Errorf("integrate: %s:\n%s\n%w", path, dirty, ErrWorkerDirty)
	}

	unlock, err := lockLandings(base)
	if err != nil {
		return nil, err
	}
	defer unlock()

	res := &IntegrateResult{BaseBranch: baseBranch, WorkerBranch: workerBranch}
	for res.Attempts < attempts {
		res.Attempts++
		baseTip, err := gitOut(base, "rev-parse", "--verify", "refs/heads/"+baseBranch)
		if err != nil {
			return nil, fmt.Errorf("integrate: resolve %s: %w", baseBranch, err)
		}
		workerTip, err := gitOut(base, "rev-parse", "--verify", "refs/heads/"+workerBranch)
		if err != nil {
			return nil, fmt.Errorf("integrate: resolve %s: %w", workerBranch, err)
		}
		res.From, res.To, res.Merge, res.Landed = baseTip, baseTip, "", nil

		if isAncestor(base, workerTip, baseTip) {
			return res, nil // nothing the shared branch does not already have
		}
		landed, err := gitOut(base, "rev-list", baseTip+".."+workerTip)
		if err != nil {
			return nil, fmt.Errorf("integrate: list commits: %w", err)
		}
		res.Landed = strings.Fields(landed)

		target := workerTip
		if !isAncestor(base, baseTip, workerTip) {
			// Both sides moved: build the merge without a working tree. Exit 1
			// is git's "conflicts", and the rest of the output names them.
			out, code, err := gitRun(base, "merge-tree", "--write-tree", "--name-only", "--no-messages", baseTip, workerTip)
			if err != nil && code != 1 {
				return nil, fmt.Errorf("integrate: merge-tree: %w: %s", err, out)
			}
			lines := strings.Split(out, "\n")
			if code == 1 {
				return nil, fmt.Errorf("integrate: %s into %s, conflicting paths:\n%s\n%w",
					workerBranch, baseBranch, strings.Join(lines[1:], "\n"), ErrConflict)
			}
			msg := fmt.Sprintf("Merge branch '%s' into %s", workerBranch, baseBranch)
			merge, err := gitOut(base, "commit-tree", lines[0], "-p", baseTip, "-p", workerTip, "-m", msg)
			if err != nil {
				return nil, fmt.Errorf("integrate: commit-tree: %w", err)
			}
			target = merge
			res.Merge = merge
		}

		// The compare-and-swap. A fast-forward from exactly baseTip is the
		// only move git will make here; anything else is refused whole.
		out, _, ffErr := gitRun(base, "merge", "--ff-only", "--quiet", target)
		if ffErr == nil {
			res.To = target
			return res, nil
		}
		nowTip, _ := gitOut(base, "rev-parse", "--verify", "refs/heads/"+baseBranch)
		if nowTip != baseTip {
			continue // someone landed in between: recompute from the new tip
		}
		if strings.Contains(out, "index.lock") {
			time.Sleep(lockBackoff) // someone holds the shared index: wait, recompute
			continue
		}
		if strings.Contains(out, "would be overwritten") {
			return nil, fmt.Errorf("integrate: %s: %s\n%w", base, out, ErrBaseOverlap)
		}
		return nil, fmt.Errorf("integrate: fast-forward %s to %s: %w: %s", baseBranch, short(target), ffErr, out)
	}
	return nil, fmt.Errorf("integrate: %s after %d attempts: %w", baseBranch, res.Attempts, ErrLostRace)
}

// lockLandings takes the repo-wide landing lock, waiting for any other
// Integrate (in this process or another) to finish. The lock file lives in
// the git common dir, which every worktree of the repo shares.
func lockLandings(base string) (func(), error) {
	common, err := gitOut(base, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("integrate: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(common, "jevons-integrate.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("integrate: open landing lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("integrate: take landing lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// IsLinkedWorktree reports whether dir is a linked worktree (git's
// --git-dir differs from its --git-common-dir) rather than a repo's main
// working tree. Ensure refuses to nest a worker's tree inside one.
func IsLinkedWorktree(dir string) bool {
	gd, err1 := gitOut(dir, "rev-parse", "--path-format=absolute", "--git-dir")
	cd, err2 := gitOut(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	return err1 == nil && err2 == nil && gd != cd
}

func isAncestor(dir, a, b string) bool {
	_, _, err := gitRun(dir, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

func gitOut(dir string, args ...string) (string, error) {
	out, _, err := gitRun(dir, args...)
	if err != nil {
		return strings.TrimSpace(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return out, nil
}

// gitRun returns trimmed combined output and the exit code, so callers can
// tell git's documented non-zero answers (merge-tree's 1) from failures.
func gitRun(dir string, args ...string) (string, int, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	b, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	return strings.TrimSpace(string(b)), code, err
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

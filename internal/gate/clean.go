// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// This file is 🎯T397: a gate must measure the worker's own commit, not the
// shared clone that happens to be sitting on top of it.
//
// # The defect
//
// A dozen workers share one working tree. jv-t381 landed 8297ae6, ran
// `go test ./internal/server/ -count=1` in that tree, and reported green
// honestly. Four readings taken afterwards in isolated worktrees:
//
//	clean checkout of 8297ae6^   EXIT=0
//	clean checkout of 8297ae6    EXIT=1   three tests failing
//	clean checkout of master     EXIT=1   the same three
//	the shared dirty tree        EXIT=0
//
// The commit had carried a neighbour's in-flight production hunk without that
// neighbour's still-uncommitted test updates. So master was red for every
// clean checkout — CI, a bisect, a fresh clone, a release build — while every
// worker in the clone read green. Nobody lied and nothing was skipped; the
// reading was simply taken in a tree that cannot show the failure.
//
// # The mechanism
//
// RunClean checks the commit out into its own detached worktree and runs the
// gate there. What the command sees is exactly what a fresh clone would see:
// the neighbour's uncommitted file is absent, so a commit that depends on it
// fails, which is the truth being sought.
//
// Two properties matter more than the worktree itself:
//
//  1. It fails rather than falls back. If the worktree cannot be created,
//     RunClean returns an error and records nothing. Quietly running in the
//     shared tree instead would reproduce the defect while wearing the name of
//     the fix — the worst available outcome, because the record would then
//     carry a clean-tree claim it did not earn.
//  2. Every run says which tree it measured. Run (the shared-tree path)
//     probes too, so a record distinguishes "measured a clean checkout of
//     abc1234", "measured a tree with 41 uncommitted changes on top of
//     abc1234", and "provenance unknown" — the last being every record
//     written before this existed. FlagFalseGreen can then flag the middle
//     case at report time without ever guessing about the third.

// dirtySampleCap is how many uncommitted paths a record keeps. Enough to
// recognise whose work was in the tree, short enough that the record stays a
// record rather than a copy of `git status`.
const dirtySampleCap = 5

// commitPrefix is how much of a commit sha goes in an attestation.
const commitPrefix = 12

// TreeProvenance is what a gate actually measured: which commit, and whether
// anything else was in the tree at the time.
//
// A nil *TreeProvenance means "not established" and is never the same answer
// as clean. Every record written before 🎯T397 has one, and a check that read
// absence as cleanliness would vouch for exactly the runs it knows least
// about.
type TreeProvenance struct {
	// Repo is the working tree the command ran in.
	Repo string `json:"repo,omitempty"`
	// Commit is the commit that tree was checked out at.
	Commit string `json:"commit,omitempty"`
	// Clean says the tree held that commit and nothing else. It is derived
	// from DirtyFiles rather than asserted by the caller: a flag a caller can
	// set is a flag a caller can be wrong about.
	Clean bool `json:"clean"`
	// DirtyFiles counts uncommitted changes present in the measured tree,
	// tracked modifications and unignored new files alike — a neighbour's
	// untracked .go file joins the build just as surely as an edited one.
	DirtyFiles  int      `json:"dirty_files,omitempty"`
	DirtySample []string `json:"dirty_sample,omitempty"`
	// Shared is the clone a clean run deliberately did not measure, so the
	// record says both what was tested and what was left out.
	Shared *SharedTreeState `json:"shared,omitempty"`
}

// SharedTreeState is the shared clone's condition at the moment a clean run
// stepped out of it.
type SharedTreeState struct {
	Repo        string   `json:"repo,omitempty"`
	DirtyFiles  int      `json:"dirty_files,omitempty"`
	DirtySample []string `json:"dirty_sample,omitempty"`
}

// ShortCommit is the commit as it appears in an attestation.
func (t *TreeProvenance) ShortCommit() string {
	if t == nil || len(t.Commit) <= commitPrefix {
		if t == nil {
			return ""
		}
		return t.Commit
	}
	return t.Commit[:commitPrefix]
}

// Token is the attestation fragment naming the tree, or "" when provenance
// was not established. Appended after the existing fields, so every reader and
// every regex that predates it goes on working.
//
//	tree=clean@8297ae6b1d9e
//	tree=dirty+41@8297ae6b1d9e
func (t *TreeProvenance) Token() string {
	if t == nil || t.Commit == "" {
		return ""
	}
	if t.Clean {
		return "tree=clean@" + t.ShortCommit()
	}
	return fmt.Sprintf("tree=dirty+%d@%s", t.DirtyFiles, t.ShortCommit())
}

// Describe is the human sentence for a Summary.
func (t *TreeProvenance) Describe() string {
	switch {
	case t == nil || t.Commit == "":
		return ""
	case t.Clean && t.Shared != nil && t.Shared.DirtyFiles > 0:
		return fmt.Sprintf("clean checkout of %s — the %d uncommitted change(s) in %s were deliberately not measured (🎯T397)",
			t.ShortCommit(), t.Shared.DirtyFiles, t.Shared.Repo)
	case t.Clean:
		return fmt.Sprintf("clean checkout of %s", t.ShortCommit())
	default:
		return fmt.Sprintf("the shared tree at %s with %d uncommitted change(s) on top of it, so this run did not measure %s alone (🎯T397): %s",
			t.ShortCommit(), t.DirtyFiles, t.ShortCommit(), strings.Join(t.DirtySample, ", "))
	}
}

// gitOut runs a git command and returns its trimmed stdout.
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// ProbeTree reports what tree dir is, best effort. A directory that is not a
// git work tree — which is where most hermetic tests run — answers nil, and
// nil is "unknown", not "clean".
func ProbeTree(dir string) *TreeProvenance {
	root, err := gitOut(dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return nil
	}
	commit, err := gitOut(dir, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	status, err := gitOut(dir, "status", "--porcelain")
	if err != nil {
		return nil
	}
	t := &TreeProvenance{Repo: root, Commit: commit}
	t.DirtyFiles, t.DirtySample = countDirty(status)
	t.Clean = t.DirtyFiles == 0
	return t
}

// countDirty reads `git status --porcelain` output. Untracked files count:
// a neighbour's new .go file is compiled into the package exactly like an
// edited one, and 🎯T360 is the sibling target about a build that only works
// because of a file the commit does not carry.
func countDirty(status string) (int, []string) {
	var n int
	var sample []string
	for line := range strings.SplitSeq(status, "\n") {
		if len(line) < 4 {
			continue
		}
		n++
		if len(sample) < dirtySampleCap {
			sample = append(sample, strings.TrimSpace(line[2:]))
		}
	}
	return n, sample
}

// CleanArgs configures a gate run against a clean checkout of one commit.
type CleanArgs struct {
	// Command is the argv to execute inside the checkout.
	Command []string
	// Repo is the clone to take the commit from; any directory inside it will
	// do. Empty means the current directory.
	Repo string
	// Commit is what to verify. Empty means the repo's HEAD, which is the
	// worker's own commit and the case this exists for.
	Commit string
	// WorkDir is where the ephemeral worktree is created. Empty means the
	// system temp directory.
	WorkDir string
	// Keep leaves the worktree behind for inspection after a failure.
	Keep bool

	// The remaining fields are handed straight to Run.
	Name           string
	Stdout, Stderr writer
	Store          *Store
	Explicit       bool
	Now            func() time.Time
}

// writer is io.Writer, aliased so this file does not import io for one field.
type writer = interface{ Write([]byte) (int, error) }

// CleanResult carries the worktree path alongside the record, so a caller that
// asked to Keep one can say where it is.
type CleanResult struct {
	Record   *Record
	Worktree string
	Kept     bool
}

// RunClean checks Commit out into its own worktree and runs the gate there.
//
// The error return means the clean tree could not be established. It is
// deliberately not survivable: there is no fallback to the shared tree,
// because a run that silently measured the wrong tree while carrying this
// function's name is worse than no run at all.
func RunClean(args *CleanArgs) (*CleanResult, error) {
	if args == nil || len(args.Command) == 0 {
		return nil, fmt.Errorf("gate clean: no command")
	}
	root, err := gitOut(args.Repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("gate clean: %s is not a git work tree: %w", repoLabel(args.Repo), err)
	}
	ref := args.Commit
	if ref == "" {
		ref = "HEAD"
	}
	commit, err := gitOut(root, "rev-parse", ref)
	if err != nil {
		return nil, fmt.Errorf("gate clean: cannot resolve %s: %w", ref, err)
	}

	shared := &SharedTreeState{Repo: root}
	if status, err := gitOut(root, "status", "--porcelain"); err == nil {
		shared.DirtyFiles, shared.DirtySample = countDirty(status)
	}

	// git refuses to check out into a non-empty directory, so the worktree is
	// a child of the scratch directory rather than the scratch directory.
	scratch, err := os.MkdirTemp(args.WorkDir, "gate-clean-")
	if err != nil {
		return nil, fmt.Errorf("gate clean: scratch dir: %w", err)
	}
	wt := filepath.Join(scratch, "tree")
	if out, err := runGit(root, "worktree", "add", "--detach", wt, commit); err != nil {
		os.RemoveAll(scratch)
		return nil, fmt.Errorf("gate clean: check out %s: %w: %s", shortCommit(commit), err, out)
	}
	// 🎯T440: stamp the owner before running anything. A gate that dies on a
	// timeout, a SIGKILL or a dropped session skips every cleanup path this
	// function has, and the leaked directory still looks healthy to
	// `git worktree prune`. The mark is the half of the lifetime that survives
	// this process; the sweeper decides the rest from outside.
	if err := worktreereap.Mark(&worktreereap.MarkArgs{
		Worktree: wt,
		Note:     "gate -clean " + gateName(args.Name, args.Command),
	}); err != nil {
		// Not fatal: an unmarked worktree is reapable by the weaker signals,
		// while refusing to run the gate over a bookkeeping failure would
		// trade the whole verification for a cleanup hint.
		fmt.Fprintln(os.Stderr, "gate clean: could not mark worktree owner:", err)
	}

	rec, runErr := Run(&RunArgs{
		Command:    args.Command,
		Dir:        wt,
		Name:       args.Name,
		Stdout:     args.Stdout,
		Stderr:     args.Stderr,
		Store:      args.Store,
		Explicit:   args.Explicit,
		SharedTree: shared,
		Now:        args.Now,
	})

	res := &CleanResult{Record: rec, Worktree: wt}
	keep := args.Keep
	if keep {
		res.Kept = true
	} else {
		removeWorktree(root, wt, scratch)
	}
	return res, runErr
}

// removeWorktree takes the checkout back out. Failures are reported and not
// returned: the gate's verdict is the thing the caller came for, and 🎯T440's
// sweeper exists precisely because this cleanup cannot be relied on.
func removeWorktree(root, wt, scratch string) {
	if out, err := runGit(root, "worktree", "remove", "--force", wt); err != nil {
		fmt.Fprintf(os.Stderr, "gate clean: worktree remove %s: %v: %s\n", wt, err, out)
	}
	if err := os.RemoveAll(scratch); err != nil {
		fmt.Fprintln(os.Stderr, "gate clean: remove scratch:", err)
	}
	if out, err := runGit(root, "worktree", "prune"); err != nil {
		fmt.Fprintf(os.Stderr, "gate clean: worktree prune: %v: %s\n", err, out)
	}
}

// runGit runs a git command for its effect, returning combined output for the
// error message.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func repoLabel(dir string) string {
	if dir == "" {
		return "the current directory"
	}
	return dir
}

func shortCommit(s string) string {
	if len(s) > commitPrefix {
		return s[:commitPrefix]
	}
	return s
}

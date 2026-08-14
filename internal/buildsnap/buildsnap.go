// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package buildsnap builds the daily daemon from a committed snapshot of the
// repository instead of the shared working tree (🎯T254.2).
//
// THE FAULT. A dozen fleet workers share one clone. `make -C $ROOT bin/jevonsd`
// therefore compiles whatever every one of them happens to have half-written,
// and a single worker's in-flight edit takes the whole fleet's daemon down with
// it: on 2026-08-09 a detached restart-daily could not rebuild at all because
// another worker's uncommitted chat.go called s.overseerStreamAccSnapshot,
// which did not exist. The worker who ran the restart had nothing to do with
// that file, could not fix it, and had no way to activate their own landed
// change — 🎯T194's whole gate, blocked by someone else's typing.
//
// WHY A COMMIT AND NOT A QUIESCENCE HEURISTIC. The sibling question — can the
// daemon avoid *serving* a mid-edit tree — was answered by looking at the real
// incident: at 21:47 the cockpit was served an index.html that was complete on
// disk, parsed cleanly, and was syntactically valid, with a call written and
// its definition not yet written. The tree was stable and wrong. No debounce
// short enough to keep the owner in their cockpit would have withheld it,
// because nothing was being written at the moment it was read. Nothing outside
// the editor can decide whether an edit is finished. A commit is the only
// artefact here that carries that guarantee, which is the same conclusion
// 🎯T376 and 🎯T377 reached from the write side.
//
// THE TRADEOFF, STATED. Building HEAD means uncommitted work is not built. That
// is deliberate and is the point: it is what makes one worker's edits unable to
// break another's activation. Commit, then restart. Run reports the HEAD it
// built and warns when the shared tree has uncommitted Go changes, so nobody
// mistakes "my change is not in the binary" for a mystery.
//
// Why Go and not more shell: restart-daily-jevonsd.sh is already 463 lines, and
// this adds worktree lifecycle state plus a real error taxonomy (git failure /
// build failure / install failure, each with a different correct response).
// Both belong in a program per the shared bash doctrine, which is also why the
// restart lock became cmd/runlock.
package buildsnap

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config describes one snapshot build.
type Config struct {
	// RepoRoot is the shared clone. Only its git HEAD is read; its working
	// tree is never compiled and never modified.
	RepoRoot string
	// SnapDir is the detached worktree used for building. Kept between runs
	// so the Go build cache stays warm; recreated whenever it is unusable.
	SnapDir string
	// Target is the make target to build inside the snapshot.
	Target string
	// Artifact is the built file's path relative to the snapshot root.
	Artifact string
	// Dest is where the artifact is installed in the shared clone.
	Dest string
	// Log receives progress lines. nil discards them.
	Log func(string)
}

// Result reports what was built, for the caller's log and for 🎯T194 evidence.
type Result struct {
	HEAD          string // commit the binary was built from
	Recreated     bool   // the snapshot worktree had to be rebuilt from scratch
	DirtyRepoRoot bool   // the shared tree had uncommitted Go changes, which were NOT built
}

func (c Config) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(fmt.Sprintf(format, args...))
	}
}

// git runs a git command and returns trimmed stdout. Stderr is folded into the
// error because a git failure here is always something the operator must read.
func git(dir string, args ...string) (string, error) {
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

// Run builds Config.Target from RepoRoot's HEAD and installs the artifact.
//
// Nothing is installed unless the build succeeded: a failed build leaves the
// previous binary exactly where it was. That matters more than it looks —
// silently keeping a stale binary while reporting success is the failure
// 🎯T194 exists to prevent, so the error is returned rather than swallowed.
func Run(cfg Config) (Result, error) {
	var res Result

	head, err := git(cfg.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return res, fmt.Errorf("read HEAD of %s: %w", cfg.RepoRoot, err)
	}
	res.HEAD = head

	if dirty, err := hasUncommittedGo(cfg.RepoRoot); err == nil && dirty {
		res.DirtyRepoRoot = true
		cfg.logf("note: the shared tree has uncommitted Go changes; they are NOT in this build. Commit, then restart.")
	}

	recreated, err := prepareSnapshot(cfg, head)
	if err != nil {
		return res, err
	}
	res.Recreated = recreated
	cfg.logf("snapshot %s at %s (recreated=%v)", cfg.SnapDir, shortSHA(head), recreated)

	src := filepath.Join(cfg.SnapDir, filepath.FromSlash(cfg.Artifact))

	// Drop the previous artifact so make cannot decide it is already current.
	//
	// This is not belt-and-braces. make compares mtimes at ONE-SECOND
	// granularity, and `git checkout` stamps the files it rewrites with the
	// current time — so a snapshot reused within the same second as its last
	// build reports "`bin/jevonsd' is up to date" and buildsnap would install
	// the OLD binary while truthfully logging the NEW head. A stale daemon
	// activated under a fresh commit's name is precisely the failure 🎯T194
	// exists to prevent, and it would be invisible. Removing the output costs
	// a link, not a compile: Go's build cache is keyed by content, not mtime.
	if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
		return res, fmt.Errorf("clear stale artifact %s: %w", src, err)
	}

	if err := runMake(cfg); err != nil {
		return res, err
	}

	if err := install(src, cfg.Dest); err != nil {
		return res, fmt.Errorf("install %s -> %s: %w", src, cfg.Dest, err)
	}
	cfg.logf("installed %s from %s", cfg.Dest, shortSHA(head))
	return res, nil
}

// hasUncommittedGo reports whether the shared tree has uncommitted changes to
// files that affect the build. Informational only — it must never block a
// build, because not being blocked by other people's edits is the entire point.
func hasUncommittedGo(root string) (bool, error) {
	out, err := git(root, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	for line := range strings.SplitSeq(out, "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "go.mod") || strings.HasSuffix(path, "go.sum") {
			return true, nil
		}
	}
	return false, nil
}

// prepareSnapshot leaves SnapDir as a detached worktree at head.
//
// Reuse is the fast path: an existing worktree keeps the build cache warm and
// only needs its checkout moved. Anything unexpected — not a worktree, dirty,
// checkout refused — is met by discarding and recreating rather than by
// forcing the tree into shape. `git reset --hard` is never run here: the
// snapshot is disposable, so deleting it is both safer and simpler than
// clobbering a directory whose contents we have not inspected.
func prepareSnapshot(cfg Config, head string) (recreated bool, err error) {
	if usable(cfg.SnapDir) {
		if _, err := git(cfg.SnapDir, "checkout", "--detach", head); err == nil {
			return false, nil
		}
		cfg.logf("snapshot at %s would not check out %s; recreating", cfg.SnapDir, head)
	}

	if err := os.RemoveAll(cfg.SnapDir); err != nil {
		return false, fmt.Errorf("remove stale snapshot %s: %w", cfg.SnapDir, err)
	}
	// Drops the administrative entry the removed directory left behind;
	// without it `git worktree add` refuses the same path.
	if _, err := git(cfg.RepoRoot, "worktree", "prune"); err != nil {
		return false, fmt.Errorf("prune worktrees: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SnapDir), 0o755); err != nil {
		return false, fmt.Errorf("create snapshot parent: %w", err)
	}
	// 🎯T440 //worktreereap:exempt — the snapshot is a permanent worktree the
	// sweeper protects by path, and it outlives every process that recreates
	// it. Stamping it with the pid of a daemon that restarts several times a
	// day would record an owner that is dead by design, turning a
	// deliberately long-lived tree into one that looks abandoned every hour.
	if _, err := git(cfg.RepoRoot, "worktree", "add", "--detach", cfg.SnapDir, head); err != nil {
		return false, fmt.Errorf("create snapshot worktree: %w", err)
	}
	return true, nil
}

// usable reports whether dir is an existing worktree with a clean tree. A
// dirty snapshot is treated as unusable: something wrote into it, and building
// it would reintroduce the very "compile whatever is lying around" behaviour
// this package removes.
func usable(dir string) bool {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	if _, err := git(dir, "rev-parse", "--git-dir"); err != nil {
		return false
	}
	out, err := git(dir, "status", "--porcelain")
	return err == nil && out == ""
}

// runMake builds inside the snapshot. Output is streamed to the caller's
// stderr so a compile error is read in the restart log, where the operator is
// already looking.
func runMake(cfg Config) error {
	cmd := exec.Command("make", "-C", cfg.SnapDir, cfg.Target)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build %s in snapshot %s: %w", cfg.Target, cfg.SnapDir, err)
	}
	return nil
}

// install copies src over dst through a temporary file in dst's directory and
// renames it into place, so a reader never sees a half-written binary and a
// failed copy cannot destroy the working one.
func install(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".buildsnap-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, fi.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

// ErrNoRepo is returned when RepoRoot is not a git repository, so the caller
// can distinguish a misconfigured invocation from a build failure.
var ErrNoRepo = errors.New("not a git repository")

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

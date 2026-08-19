// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitbase_test

// End-to-end oracle for 🎯T432 over a real git repository.
//
// It replays the incident that produced e66e934: a worker seeds a private
// index from HEAD, another worker's commit lands in the window before
// commit-tree, and a tree built from the stale seed deletes the interloper's
// paths when the commit's parent is re-read as current HEAD.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitbase"
)

const (
	workerPath     = "gate/runner.go"
	interloperPath = "cmd/detach/main.go"
	hotFile        = "Makefile"
)

// TestBlessedRecipePreservesInterloperPaths is acceptance criterion 4.
//
// A second commit lands between read-tree and commit-tree. The blessed
// recipe refuses rather than writing a stale tree; after the worker
// recovers (re-seed + re-stage), the resulting commit carries both its own
// path and the interloper's. The stale-snapshot control is asserted red in
// TestStaleSnapshotRecipeDeletesInterloperPaths.
func TestBlessedRecipePreservesInterloperPaths(t *testing.T) {
	repo := newRepo(t)
	repo.write(t, workerPath, "worker's gate change")

	var interloperSHA string
	res, err := commitbase.Commit(&commitbase.CommitArgs{
		Dir:     repo.dir,
		Message: "feat(gate): worker's own change",
		Paths:   []string{workerPath},
		AfterSeed: func() error {
			// Interloper lands between seed and commit — the e66e934 window.
			repo.write(t, interloperPath, "detach self-reexec (🎯T405)")
			out, err := repo.git(t, "add", interloperPath)
			if err != nil {
				return err
			}
			out, err = repo.git(t, "commit", "-m", "feat(T405): detach + supervise")
			if err != nil {
				return errors.New(out + ": " + err.Error())
			}
			interloperSHA = repo.head(t)
			return nil
		},
	})
	var refused *commitbase.RefuseError
	if !errors.As(err, &refused) {
		t.Fatalf("blessed recipe did not refuse on moved HEAD: res=%v err=%v", res, err)
	}
	if !strings.Contains(refused.Message, interloperPath) {
		t.Errorf("refusal does not name the interloper path %q:\n%s", interloperPath, refused.Message)
	}
	if !strings.Contains(refused.Message, "🎯T432") {
		t.Errorf("refusal is not attributable to the guard:\n%s", refused.Message)
	}
	if got := repo.head(t); got != interloperSHA {
		t.Fatalf("HEAD moved despite refusal: got %s want %s", got, interloperSHA)
	}
	if !repo.pathInHEAD(t, interloperPath) {
		t.Fatal("interloper path missing from HEAD after refusal")
	}

	// Recovery: re-seed from current HEAD and commit. Both paths must land.
	res, err = commitbase.Commit(&commitbase.CommitArgs{
		Dir:     repo.dir,
		Message: "feat(gate): worker's own change",
		Paths:   []string{workerPath},
	})
	if err != nil {
		t.Fatalf("recovery commit: %v", err)
	}
	stat := repo.showStat(t, res.CommitSHA)
	if !strings.Contains(stat, workerPath) {
		t.Errorf("recovery commit missing worker path:\n%s", stat)
	}
	if !repo.pathInHEAD(t, interloperPath) {
		t.Fatal("recovery commit deleted the interloper path")
	}
	if !repo.pathInHEAD(t, workerPath) {
		t.Fatal("recovery commit missing worker path in tree")
	}
}

// TestStaleSnapshotRecipeDeletesInterloperPaths is the RED control: the
// recipe that seeds from a snapshot, re-reads HEAD for parent + update-ref
// CAS, and writes anyway. That is exactly update-ref-alone, and it is the
// shape that produced e66e934.
func TestStaleSnapshotRecipeDeletesInterloperPaths(t *testing.T) {
	repo := newRepo(t)
	repo.write(t, workerPath, "worker's gate change")

	res, err := commitbase.StaleCommit(&commitbase.CommitArgs{
		Dir:     repo.dir,
		Message: "feat(gate): looks scoped, deletes interloper",
		Paths:   []string{workerPath},
		AfterSeed: func() error {
			repo.write(t, interloperPath, "detach self-reexec (🎯T405)")
			if _, err := repo.git(t, "add", interloperPath); err != nil {
				return err
			}
			_, err := repo.git(t, "commit", "-m", "feat(T405): detach + supervise")
			return err
		},
	})
	if err != nil {
		t.Fatalf("stale control failed to commit: %v", err)
	}
	if repo.pathInHEAD(t, interloperPath) {
		t.Fatalf("stale control unexpectedly preserved %s — the red oracle is broken; commit=%s",
			interloperPath, res.CommitSHA)
	}
	if !repo.pathInHEAD(t, workerPath) {
		t.Fatal("stale control also lost the worker's own path")
	}
}

// TestBlessedRecipePartialHotFileStaging covers acceptance criterion 2: the
// case `git commit --only` cannot — a shared hot file whose worktree still
// holds another worker's uncommitted hunks. The blessed recipe stages exact
// blob content for that file and leaves the foreign hunks uncommitted.
func TestBlessedRecipePartialHotFileStaging(t *testing.T) {
	repo := newRepo(t)

	// Worktree holds BOTH workers' Makefile hunks (uncommitted foreign + ours).
	foreign := "# foreign worker hunk (uncommitted)\n"
	ours := "# our worker hunk (to commit)\n"
	repo.write(t, hotFile, "base\n"+foreign+ours)
	repo.write(t, workerPath, "our non-hot change")

	ourMakefile := []byte("base\n" + ours)
	res, err := commitbase.Commit(&commitbase.CommitArgs{
		Dir:     repo.dir,
		Message: "chore: our Makefile hunk only",
		Paths:   []string{workerPath},
		Blobs:   map[string][]byte{hotFile: ourMakefile},
	})
	if err != nil {
		t.Fatalf("partial hot-file commit: %v", err)
	}

	got := repo.blobAt(t, res.CommitSHA, hotFile)
	if string(got) != string(ourMakefile) {
		t.Errorf("committed Makefile = %q, want exactly our hunks %q", got, ourMakefile)
	}
	if strings.Contains(string(got), "foreign") {
		t.Errorf("committed Makefile swept the foreign uncommitted hunk:\n%s", got)
	}
	// Foreign hunk must still be in the work tree — we staged a blob, not
	// the worktree, so we must not have clobbered it.
	wt, err := os.ReadFile(filepath.Join(repo.dir, hotFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wt), "foreign") {
		t.Errorf("worktree Makefile lost the foreign hunk:\n%s", wt)
	}
}

// TestUpdateRefCASAloneIsNotEnough shows criterion 3 directly: even with
// update-ref old/new against current HEAD, a stale tree deletes interloper
// paths. The blessed recipe's HEAD re-check is what stops that; CAS alone
// does not.
func TestUpdateRefCASAloneIsNotEnough(t *testing.T) {
	// StaleCommit is defined as "CAS against current HEAD + stale tree".
	// If that control preserves the interloper, the premise of T432 is wrong.
	repo := newRepo(t)
	repo.write(t, workerPath, "x")
	_, err := commitbase.StaleCommit(&commitbase.CommitArgs{
		Dir:     repo.dir,
		Message: "cas-alone",
		Paths:   []string{workerPath},
		AfterSeed: func() error {
			repo.write(t, interloperPath, "new")
			if _, err := repo.git(t, "add", interloperPath); err != nil {
				return err
			}
			_, err := repo.git(t, "commit", "-m", "interloper")
			return err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.pathInHEAD(t, interloperPath) {
		t.Fatal("update-ref CAS alone preserved the interloper — T432's premise is false")
	}
}

// --- harness -------------------------------------------------------------

type repo struct {
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	r := &repo{dir: t.TempDir()}
	if out, err := r.git(t, "init", "-q", "-b", "master", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, kv := range [][2]string{{"user.email", "t@example.invalid"}, {"user.name", "t"}} {
		if out, err := r.git(t, "config", kv[0], kv[1]); err != nil {
			t.Fatalf("git config: %v\n%s", err, out)
		}
	}
	r.write(t, hotFile, "base\n")
	r.write(t, "README", "base\n")
	if out, err := r.git(t, "add", "."); err != nil {
		t.Fatalf("seed add: %v\n%s", err, out)
	}
	if out, err := r.git(t, "commit", "-m", "base"); err != nil {
		t.Fatalf("seed commit: %v\n%s", err, out)
	}
	return r
}

func (r *repo) write(t *testing.T, name, content string) {
	t.Helper()
	path := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r *repo) git(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.dir,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *repo) head(t *testing.T) string {
	t.Helper()
	out, err := r.git(t, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v\n%s", err, out)
	}
	return strings.TrimSpace(out)
}

func (r *repo) showStat(t *testing.T, rev string) string {
	t.Helper()
	out, err := r.git(t, "show", "--stat", "--format=", rev)
	if err != nil {
		t.Fatalf("show --stat: %v\n%s", err, out)
	}
	return out
}

func (r *repo) pathInHEAD(t *testing.T, path string) bool {
	t.Helper()
	out, err := r.git(t, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		t.Fatalf("ls-tree: %v\n%s", err, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == path {
			return true
		}
	}
	return false
}

func (r *repo) blobAt(t *testing.T, rev, path string) string {
	t.Helper()
	out, err := r.git(t, "show", rev+":"+path)
	if err != nil {
		t.Fatalf("show %s:%s: %v\n%s", rev, path, err, out)
	}
	return out
}

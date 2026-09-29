// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// Oracles for the 🎯T254.2 single integrator: worker commits made in isolated
// trees land on the shared clone's branch, and the landing never leaves the
// shared clone worse than it found it.

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// sharedClone makes a repo on master with two tracked files, the way the
// shared clone looks before a fan-out. It lives one level down so its
// .jevons-worktrees-* sibling stays inside the test's temp dir.
func sharedClone(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	base := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, base, "init", "-q", "-b", "master")
	git(t, base, "config", "user.email", "t@example.com")
	git(t, base, "config", "user.name", "t")
	writeFile(t, filepath.Join(base, "a.txt"), "a\n")
	writeFile(t, filepath.Join(base, "b.txt"), "b\n")
	git(t, base, "add", ".")
	git(t, base, "commit", "-q", "-m", "seed")
	return base
}

// workerCommit makes the worker's isolated tree (as jevons_agent_start does)
// and commits content to file in it, returning the tree and the commit.
func workerCommit(t *testing.T, base, agent, file, content string) (string, string) {
	t.Helper()
	wt, err := worktree.Ensure(base, agent)
	if err != nil {
		t.Fatalf("Ensure(%s): %v", agent, err)
	}
	writeFile(t, filepath.Join(wt, file), content)
	git(t, wt, "add", file)
	git(t, wt, "commit", "-q", "-m", agent+" edits "+file)
	return wt, git(t, wt, "rev-parse", "HEAD")
}

func mustReach(t *testing.T, base, sha string) {
	t.Helper()
	if err := exec.Command("git", "-C", base, "merge-base", "--is-ancestor", sha, "master").Run(); err != nil {
		t.Fatalf("worker commit %s is not reachable from master: %v", sha, err)
	}
}

func TestIntegrateFastForwardsAndKeepsWorkerSHA(t *testing.T) {
	base := sharedClone(t)
	from := git(t, base, "rev-parse", "master")
	_, sha := workerCommit(t, base, "jv-alpha", "a.txt", "alpha\n")

	res, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-alpha"})
	if err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	if res.Merge != "" || res.To != sha || res.From != from || len(res.Landed) != 1 || res.Landed[0] != sha {
		t.Fatalf("want fast-forward %s..%s landing [%s], got %+v", from, sha, sha, res)
	}
	mustReach(t, base, sha)
	if got := readFile(t, filepath.Join(base, "a.txt")); got != "alpha\n" {
		t.Fatalf("shared clone's working tree not updated by the landing: a.txt = %q", got)
	}
	if st := git(t, base, "status", "--porcelain"); st != "" {
		t.Fatalf("landing left the shared clone dirty:\n%s", st)
	}

	// Landing again is a no-op, not an error.
	again, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-alpha"})
	if err != nil || len(again.Landed) != 0 || again.To != sha {
		t.Fatalf("re-integrate: want nothing to land at %s, got %+v err=%v", sha, again, err)
	}
}

func TestIntegrateMergesWhenBothSidesMoved(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerCommit(t, base, "jv-alpha", "a.txt", "alpha\n")
	// Master moves on another path after the worker branched.
	writeFile(t, filepath.Join(base, "b.txt"), "master\n")
	git(t, base, "commit", "-q", "-am", "master moves")
	masterTip := git(t, base, "rev-parse", "master")

	res, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-alpha"})
	if err != nil {
		t.Fatalf("Integrate: %v", err)
	}
	if res.Merge == "" || res.To != res.Merge {
		t.Fatalf("want a merge commit, got %+v", res)
	}
	parents := strings.Fields(git(t, base, "rev-list", "--parents", "-n", "1", res.Merge))
	if len(parents) != 3 || parents[1] != masterTip || parents[2] != sha {
		t.Fatalf("merge parents = %v, want [%s %s]", parents[1:], masterTip, sha)
	}
	mustReach(t, base, sha)
	if a, b := readFile(t, filepath.Join(base, "a.txt")), readFile(t, filepath.Join(base, "b.txt")); a != "alpha\n" || b != "master\n" {
		t.Fatalf("merged tree lost a side: a.txt=%q b.txt=%q", a, b)
	}
	if st := git(t, base, "status", "--porcelain"); st != "" {
		t.Fatalf("landing left the shared clone dirty:\n%s", st)
	}
}

func TestIntegrateConflictTouchesNothing(t *testing.T) {
	base := sharedClone(t)
	workerCommit(t, base, "jv-alpha", "a.txt", "alpha\n")
	writeFile(t, filepath.Join(base, "a.txt"), "master\n")
	git(t, base, "commit", "-q", "-am", "master edits a")
	tip := git(t, base, "rev-parse", "master")

	_, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-alpha"})
	if !errors.Is(err, worktree.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("conflict refusal does not name the path: %v", err)
	}
	if now := git(t, base, "rev-parse", "master"); now != tip {
		t.Fatalf("a refused landing moved master %s -> %s", tip, now)
	}
	if got := readFile(t, filepath.Join(base, "a.txt")); got != "master\n" {
		t.Fatalf("a refused landing wrote into the shared tree: a.txt = %q", got)
	}
	if st := git(t, base, "status", "--porcelain"); st != "" {
		t.Fatalf("a refused landing left the shared clone dirty:\n%s", st)
	}
}

// Another worker's uncommitted and staged edits in the shared clone are not
// the integrator's to touch: unrelated ones survive a landing, and one on a
// landed path refuses the landing whole.
func TestIntegratePreservesSharedCloneEdits(t *testing.T) {
	base := sharedClone(t)
	_, sha := workerCommit(t, base, "jv-alpha", "a.txt", "alpha\n")
	writeFile(t, filepath.Join(base, "b.txt"), "someone's uncommitted edit\n")
	writeFile(t, filepath.Join(base, "c.txt"), "someone's staged file\n")
	git(t, base, "add", "c.txt")

	if _, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-alpha"}); err != nil {
		t.Fatalf("Integrate with unrelated shared-clone edits: %v", err)
	}
	mustReach(t, base, sha)
	if got := readFile(t, filepath.Join(base, "b.txt")); got != "someone's uncommitted edit\n" {
		t.Fatalf("landing clobbered an uncommitted edit: b.txt = %q", got)
	}
	if staged := git(t, base, "diff", "--cached", "--name-only"); staged != "c.txt" {
		t.Fatalf("landing disturbed the shared index: staged = %q", staged)
	}
}

func TestIntegrateRefusesOverlapWithSharedCloneEdit(t *testing.T) {
	base := sharedClone(t)
	workerCommit(t, base, "jv-alpha", "a.txt", "alpha\n")
	writeFile(t, filepath.Join(base, "a.txt"), "someone's uncommitted edit\n")
	tip := git(t, base, "rev-parse", "master")

	_, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-alpha"})
	if !errors.Is(err, worktree.ErrBaseOverlap) {
		t.Fatalf("want ErrBaseOverlap, got %v", err)
	}
	if now := git(t, base, "rev-parse", "master"); now != tip {
		t.Fatalf("a refused landing moved master %s -> %s", tip, now)
	}
	if got := readFile(t, filepath.Join(base, "a.txt")); got != "someone's uncommitted edit\n" {
		t.Fatalf("a refused landing lost the uncommitted edit: a.txt = %q", got)
	}
}

func TestIntegrateRefusesDirtyWorkerTree(t *testing.T) {
	base := sharedClone(t)
	wt, _ := workerCommit(t, base, "jv-alpha", "a.txt", "alpha\n")
	writeFile(t, filepath.Join(wt, "b.txt"), "forgot to commit\n")

	_, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-alpha"})
	if !errors.Is(err, worktree.ErrWorkerDirty) || !strings.Contains(err.Error(), "b.txt") {
		t.Fatalf("want ErrWorkerDirty naming b.txt, got %v", err)
	}
}

func TestIntegrateRefusesUnisolatedWorkerAndLinkedBase(t *testing.T) {
	base := sharedClone(t)
	if _, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: base, AgentName: "jv-nobody"}); !errors.Is(err, worktree.ErrNotIsolated) {
		t.Fatalf("want ErrNotIsolated for a worker with no tree, got %v", err)
	}
	wt, _ := workerCommit(t, base, "jv-alpha", "a.txt", "alpha\n")
	if _, err := worktree.Integrate(&worktree.IntegrateArgs{BaseWorkdir: wt, AgentName: "jv-alpha"}); !errors.Is(err, worktree.ErrBaseIsWorktree) {
		t.Fatalf("want ErrBaseIsWorktree integrating into a worker's tree, got %v", err)
	}
	if _, err := worktree.Ensure(wt, "jv-nested"); !errors.Is(err, worktree.ErrBaseIsWorktree) {
		t.Fatalf("want Ensure to refuse nesting a tree in a worker's tree, got %v", err)
	}
}

// Several integrators landing at once each land their worker, and none of
// them loses another's commit: the fast-forward is the compare-and-swap.
func TestIntegrateConcurrentLandingsAllLand(t *testing.T) {
	base := sharedClone(t)
	const workers = 4
	shas := make([]string, workers)
	for i := range workers {
		_, shas[i] = workerCommit(t, base, fmt.Sprintf("jv-w%d", i), fmt.Sprintf("w%d.txt", i), fmt.Sprintf("w%d\n", i))
	}
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = worktree.Integrate(&worktree.IntegrateArgs{
				BaseWorkdir: base,
				AgentName:   fmt.Sprintf("jv-w%d", i),
				// The default: the landing lock, not retries, is what makes this pass.
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("jv-w%d: %v", i, err)
		}
	}
	for i, sha := range shas {
		mustReach(t, base, sha)
		if got := readFile(t, filepath.Join(base, fmt.Sprintf("w%d.txt", i))); got != fmt.Sprintf("w%d\n", i) {
			t.Fatalf("w%d.txt = %q after concurrent landings", i, got)
		}
	}
	if st := git(t, base, "status", "--porcelain"); st != "" {
		t.Fatalf("concurrent landings left the shared clone dirty:\n%s", st)
	}
}

func TestSharesRepoAndExisting(t *testing.T) {
	base := "/w/org/jevons"
	cases := []struct {
		other string
		want  bool
	}{
		{"/w/org/jevons", true},
		{"/w/org/jevons/", true},
		{"/w/org/.jevons-worktrees-jevons/jv-a", true},
		{"/w/org/.jevons-worktrees-jevonsx/jv-a", false},
		{"/w/org/jevons-other", false},
		{"/w/org/.jevons-worktrees-jevons", false},
		{"", false},
	}
	for _, c := range cases {
		if got := worktree.SharesRepo(base, c.other); got != c.want {
			t.Errorf("SharesRepo(%q, %q) = %v, want %v", base, c.other, got, c.want)
		}
	}

	repo := sharedClone(t)
	if _, ok := worktree.Existing(repo, "jv-alpha"); ok {
		t.Fatal("Existing reports a tree that was never made")
	}
	wt, err := worktree.Ensure(repo, "jv-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := worktree.Existing(repo, "jv-alpha"); !ok || got != wt {
		t.Fatalf("Existing = %q,%v, want %q,true", got, ok, wt)
	}
	if br := git(t, wt, "symbolic-ref", "--short", "HEAD"); br != worktree.BranchName("jv-alpha") {
		t.Fatalf("tree is on %q, want %q", br, worktree.BranchName("jv-alpha"))
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T440 //worktreereap:exempt — hermetic T1311 fixtures torn down in this
// process; a timeout/SIGKILL cannot leak them past t.TempDir.
package worktree_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

func t1311Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func t1311ForceRemove(t *testing.T, clone, path, branch string) {
	t.Helper()
	_ = exec.Command("git", "-C", clone, "worktree", "remove", "--force", path).Run()
	if branch != "" {
		_ = exec.Command("git", "-C", clone, "branch", "-D", branch).Run()
	}
}

// t1311Scratch is the 2026-10-29 isolate specimen: a scratch clone with a
// sibling improvised worktree on a feature branch (wordfreq-json / feature/json).
func t1311Scratch(t *testing.T) (clone, improvised string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	parent := t.TempDir()
	clone = filepath.Join(parent, "wordfreq")
	if err := os.Mkdir(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	t1311Git(t, clone, "init", "-q")
	t1311Git(t, clone, "checkout", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(clone, "README"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1311Git(t, clone, "add", ".")
	t1311Git(t, clone, "commit", "-q", "-m", "seed")
	improvised = filepath.Join(parent, "wordfreq-json")
	t1311Git(t, clone, "worktree", "add", "-b", "feature/json", improvised)
	t.Cleanup(func() { t1311ForceRemove(t, clone, improvised, "feature/json") })
	return clone, improvised
}

func assertTreeSurvived(t *testing.T, clone, improvised, branch string) {
	t.Helper()
	if _, err := os.Stat(improvised); err != nil {
		t.Fatalf("worktree was removed: %v", err)
	}
	listed := t1311Git(t, clone, "worktree", "list", "--porcelain")
	if !strings.Contains(listed, improvised) {
		t.Fatalf("git no longer lists the worktree:\n%s", listed)
	}
	if branch != "" {
		if got := t1311Git(t, clone, "branch", "--list", branch); strings.TrimSpace(got) == "" {
			t.Fatalf("branch %s was deleted", branch)
		}
	}
}

func TestIsManagedFleetWorktree(t *testing.T) {
	base := "/repo/wordfreq"
	managed := worktree.WorktreePath(base, "jv-t1311-worker")
	if !worktree.IsManagedFleetWorktree(managed) {
		t.Fatalf("managed path %q not recognised", managed)
	}
	if worktree.IsManagedFleetWorktree(base) {
		t.Fatal("shared clone is not a managed fleet worktree")
	}
	if worktree.IsManagedFleetWorktree("/tmp/wordfreq-json") {
		t.Fatal("improvised sibling must not look managed")
	}
}

func TestImprovisedCleanupNoteNamesRemoveAndBranch(t *testing.T) {
	_, improvised := t1311Scratch(t)
	note := worktree.ImprovisedCleanupNote(improvised)
	for _, want := range []string{
		"🎯T1311",
		"git worktree remove " + improvised,
		"git branch -d feature/json",
		"Do not leave sibling worktrees behind",
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("note missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "remove --force") || strings.Contains(note, "branch -D") {
		t.Fatalf("brief still recommends force delete:\n%s", note)
	}
}

func TestCleanupImprovisedRemovesSiblingTreeAndFeatureBranch(t *testing.T) {
	clone, improvised := t1311Scratch(t)
	if err := worktree.CleanupImprovised(improvised); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(improvised); !os.IsNotExist(err) {
		t.Fatalf("improvised worktree still on disk: %v", err)
	}
	listed := t1311Git(t, clone, "worktree", "list", "--porcelain")
	if strings.Contains(listed, improvised) {
		t.Fatalf("git still lists improvised tree:\n%s", listed)
	}
	branches := t1311Git(t, clone, "branch", "--list", "feature/json")
	if strings.TrimSpace(branches) != "" {
		t.Fatalf("feature branch survived: %q", branches)
	}
	if _, err := os.Stat(clone); err != nil {
		t.Fatalf("shared clone was removed: %v", err)
	}
	if got := t1311Git(t, clone, "symbolic-ref", "--short", "HEAD"); got != "master" {
		t.Fatalf("shared HEAD moved to %q", got)
	}
}

func TestCleanupImprovisedLeavesUnlandedBranch(t *testing.T) {
	clone, improvised := t1311Scratch(t)
	if err := os.WriteFile(filepath.Join(improvised, "json.go"), []byte("package json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1311Git(t, improvised, "add", ".")
	t1311Git(t, improvised, "commit", "-q", "-m", "unlanded json")
	if err := worktree.CleanupImprovised(improvised); err != nil {
		t.Fatal(err)
	}
	assertTreeSurvived(t, clone, improvised, "feature/json")
}

func TestCleanupImprovisedLeavesDirtyTree(t *testing.T) {
	clone, improvised := t1311Scratch(t)
	if err := os.WriteFile(filepath.Join(improvised, "scratch.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := worktree.CleanupImprovised(improvised); err != nil {
		t.Fatal(err)
	}
	assertTreeSurvived(t, clone, improvised, "feature/json")
}

func TestCleanupImprovisedLeavesNonSiblingWorktree(t *testing.T) {
	clone, _ := t1311Scratch(t)
	nested := filepath.Join(clone, ".claude", "worktrees", "owner-tree")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	t1311Git(t, clone, "worktree", "add", "-b", "feature/owner", nested)
	t.Cleanup(func() { t1311ForceRemove(t, clone, nested, "feature/owner") })
	if err := worktree.CleanupImprovised(nested); err != nil {
		t.Fatal(err)
	}
	assertTreeSurvived(t, clone, nested, "feature/owner")
}

func TestCleanupImprovisedRemovesCherryPickedBranch(t *testing.T) {
	clone, improvised := t1311Scratch(t)
	if err := os.WriteFile(filepath.Join(improvised, "json.go"), []byte("package json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1311Git(t, improvised, "add", ".")
	t1311Git(t, improvised, "commit", "-q", "-m", "json slice")
	sha := t1311Git(t, improvised, "rev-parse", "HEAD")
	t1311Git(t, clone, "cherry-pick", "-x", sha)
	if err := worktree.CleanupImprovised(improvised); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(improvised); !os.IsNotExist(err) {
		t.Fatalf("cherry-picked worktree survived: %v", err)
	}
	if got := t1311Git(t, clone, "branch", "--list", "feature/json"); strings.TrimSpace(got) != "" {
		t.Fatalf("cherry-picked branch survived: %q", got)
	}
}

func TestCleanupImprovisedLeavesManagedFleetTree(t *testing.T) {
	clone, _ := t1311Scratch(t)
	managed, err := worktree.Ensure(clone, "jv-t1311-managed")
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.CleanupImprovised(managed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatalf("managed fleet tree was removed: %v", err)
	}
	if got := t1311Git(t, managed, "symbolic-ref", "--short", "HEAD"); got != worktree.BranchName("jv-t1311-managed") {
		t.Fatalf("managed branch = %q", got)
	}
}

func TestCleanupImprovisedNoopsOnSharedClone(t *testing.T) {
	clone, _ := t1311Scratch(t)
	if err := worktree.CleanupImprovised(clone); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(clone); err != nil {
		t.Fatalf("shared clone disappeared: %v", err)
	}
}

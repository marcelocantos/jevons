// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// T1201: the accepted unrelated index/worktree dirt is not owned by the
// integrator, even when a pristine post-merge gate rejects the landing. The
// merge is gated before the branch moves, so a rejection touches nothing.
func TestIntegrateFailedGatePreservesUnrelatedDirtyState(t *testing.T) {
	{
		t.Run("gate-rejects", func(t *testing.T) {
			base := sharedClone(t)
			seed := map[string]string{
				"bullseye.yaml": "targets:\n  T1:\n    name: preserved\n    status: converging\n",
				"partial.txt":   "original\n", "staged-delete.txt": "delete me\n",
				"unstaged-delete.txt": "delete me too\n", "rename-old.txt": "rename me\n",
				"staged-only.txt": "original staged-only\n", "executable.txt": "executable\n",
			}
			for name, body := range seed {
				writeFile(t, filepath.Join(base, name), body)
			}
			git(t, base, "add", ".")
			git(t, base, "commit", "-q", "-m", "seed unrelated tracked files")
			workerCommit(t, base, "jv-rollback", "a.txt", "worker\n")
			writeFile(t, filepath.Join(base, "b.txt"), "trunk moved\n")
			git(t, base, "commit", "-q", "-am", "move trunk")
			tip := git(t, base, "rev-parse", "HEAD")
			writeFile(t, filepath.Join(base, "bullseye.yaml"), seed["bullseye.yaml"]+"  T1200:\n    name: owner work\n    status: converging\n")
			writeFile(t, filepath.Join(base, "partial.txt"), "staged version\n")
			writeFile(t, filepath.Join(base, "added.txt"), "staged addition\n")
			git(t, base, "add", "partial.txt", "added.txt")
			writeFile(t, filepath.Join(base, "staged-only.txt"), "staged only\n")
			git(t, base, "add", "staged-only.txt")
			if err := os.Chmod(filepath.Join(base, "executable.txt"), 0755); err != nil {
				t.Fatal(err)
			}
			git(t, base, "add", "executable.txt")
			writeFile(t, filepath.Join(base, "partial.txt"), "unstaged version\x00with binary bytes\n")
			writeFile(t, filepath.Join(base, "added.txt"), "unstaged addition edit\n")
			git(t, base, "rm", "staged-delete.txt")
			if err := os.Remove(filepath.Join(base, "unstaged-delete.txt")); err != nil {
				t.Fatal(err)
			}
			git(t, base, "mv", "rename-old.txt", "rename-new.txt")
			writeFile(t, filepath.Join(base, "rename-new.txt"), "unstaged renamed edit\n")
			writeFile(t, filepath.Join(base, "untracked.txt"), "private scratch\x00\n")
			before := rollbackState(t, base)
			_, err := worktree.Integrate(&worktree.IntegrateArgs{
				BaseWorkdir: base, AgentName: "jv-rollback", GateStoreRoot: t.TempDir(),
				PostMergeGateCommand: []string{"false"},
			})
			if err == nil || !strings.Contains(err.Error(), worktree.ErrPostMergeGate.Error()) || !strings.Contains(err.Error(), "NOT LANDED") {
				t.Fatalf("expected a rejected, NOT LANDED post-merge gate, got %v", err)
			}
			if now := git(t, base, "rev-parse", "HEAD"); now != tip {
				t.Fatalf("rejected landing moved HEAD=%s, want %s: %v", now, tip, err)
			}
			if after := rollbackState(t, base); after != before {
				t.Fatalf("rollback changed unrelated bytes or staging:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			assertRollbackLocksReleased(t, base)
		})
	}
}

func rollbackState(t *testing.T, base string) string {
	t.Helper()
	// Exclude only the landed path: failure compensation deliberately keeps
	// that path at the landing. All unrelated index modes and bytes must match.
	paths := []string{".", ":(exclude)a.txt"}
	var state strings.Builder
	for _, args := range [][]string{{"status", "--porcelain=v1", "-z", "--"}, {"ls-files", "--stage", "-z", "--"}, {"diff", "--binary", "--"}, {"diff", "--cached", "--binary", "--"}} {
		state.WriteString(git(t, base, append(args, paths...)...))
		state.WriteByte('\n')
	}
	for _, path := range []string{"bullseye.yaml", "partial.txt", "added.txt", "rename-new.txt", "untracked.txt"} {
		state.WriteString(path + ":" + readFile(t, filepath.Join(base, path)))
	}
	return state.String()
}

func TestIntegrateRollbackRefusesConcurrentChanges(t *testing.T) {
	for _, action := range []string{"unstaged", "staged", "branch-moved", "head-switched"} {
		t.Run(action, func(t *testing.T) {
			base := sharedClone(t)
			workerCommit(t, base, "jv-concurrent", "a.txt", "worker\n")
			writeFile(t, filepath.Join(base, "b.txt"), "trunk\n")
			git(t, base, "commit", "-q", "-am", "move trunk")
			// The gate runs against a clean checkout but another actor may
			// edit the shared clone while it runs. Arguments carry the path;
			// no shell interpolation of the test directory is needed.
			gateScript := `set -eu
case "$2" in
unstaged) printf 'new owner edit\n' > "$1/a.txt" ;;
staged) printf 'new owner edit\n' > "$1/a.txt"; git -C "$1" add a.txt ;;
branch-moved) git -C "$1" commit -q --allow-empty -m concurrent ;;
head-switched) git -C "$1" branch other; git -C "$1" symbolic-ref HEAD refs/heads/other ;;
esac
git -C "$1" rev-parse HEAD > "$1/expected-head"
exit 7`
			_, err := worktree.Integrate(&worktree.IntegrateArgs{
				BaseWorkdir: base, AgentName: "jv-concurrent", GateStoreRoot: t.TempDir(),
				PostMergeGateCommand: []string{"sh", "-c", gateScript, "gate", base, action},
			})
			if err == nil || !strings.Contains(err.Error(), "NOT LANDED") {
				t.Fatalf("a rejected gate during concurrent %s must not land: %v", action, err)
			}
			if now, want := git(t, base, "rev-parse", "HEAD"), strings.TrimSpace(readFile(t, filepath.Join(base, "expected-head"))); now != want {
				t.Fatalf("rollback moved the concurrent head: %s, want %s", now, want)
			}
			if action == "unstaged" || action == "staged" {
				if got := readFile(t, filepath.Join(base, "a.txt")); got != "new owner edit\n" {
					t.Fatalf("rollback lost overlapping edit: %q", got)
				}
				if action == "staged" && git(t, base, "show", ":a.txt") != "new owner edit" {
					t.Fatal("rollback lost overlapping staged edit")
				}
			} else if git(t, base, "diff", "HEAD", "--", "a.txt", "b.txt") != "" {
				t.Fatal("refusal left HEAD and worktree inconsistent")
			}
			assertRollbackLocksReleased(t, base)
		})
	}
}

func assertRollbackLocksReleased(t *testing.T, base string) {
	t.Helper()
	for _, path := range []string{"HEAD.lock", "refs/heads/master.lock", "index.lock"} {
		if _, err := os.Stat(filepath.Join(base, ".git", path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rollback left lock %s: %v", path, err)
		}
	}
	current := git(t, base, "rev-parse", "master")
	git(t, base, "update-ref", "refs/heads/master", current, current)
}

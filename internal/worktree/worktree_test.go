// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

func TestValidateSeatFleetWorkdir(t *testing.T) {
	base := "/repo/jevons"
	own := worktree.WorktreePath(base, "jv-t995-auto")
	sibling := worktree.WorktreePath(base, "jv-t1159-auto")
	if err := worktree.ValidateSeatFleetWorkdir("jv-t995-auto", own); err != nil {
		t.Fatalf("own tree: %v", err)
	}
	if err := worktree.ValidateSeatFleetWorkdir("jv-t995-auto", sibling); err == nil {
		t.Fatal("sibling fleet tree must be rejected")
	}
	if err := worktree.ValidateSeatFleetWorkdir("jv-t995-auto", base); err != nil {
		t.Fatalf("shared clone: %v", err)
	}
}

func TestSharedCloneFromFleetWorktree(t *testing.T) {
	base := "/repo/jevons"
	wt := worktree.WorktreePath(base, "jv-t1159-auto")
	got, ok := worktree.SharedCloneFromFleetWorktree(wt)
	if !ok {
		t.Fatal("expected fleet worktree path")
	}
	if filepath.Clean(got) != filepath.Clean(base) {
		t.Fatalf("shared clone = %q, want %q", got, base)
	}
	if _, ok := worktree.SharedCloneFromFleetWorktree(base); ok {
		t.Fatal("shared clone itself is not a fleet worktree leaf")
	}
}

func TestWorktreePathDistinctPerAgent(t *testing.T) {
	base := "/repo/jevons"
	a := worktree.WorktreePath(base, "jv-t254.2-fanout-a")
	b := worktree.WorktreePath(base, "jv-t254.2-fanout-b")
	if a == b {
		t.Fatalf("distinct agents got identical worktree path: %s", a)
	}
	if worktree.WorktreePath(base, "jv-t254.2-fanout-a") != a {
		t.Fatalf("path not deterministic for same agent name")
	}
}

func TestNeedsIsolationClassifier(t *testing.T) {
	cases := []struct {
		purpose, role string
		want        bool
	}{
		{"work", "worker", true},
		{"work", "", true}, // empty role defaults to worker semantics
		{"work", "worker", true}, // 🎯T1151.1: always isolate lone workers
		{"work", "boss", false},
		{"work", "product-owner", false},
		{"aside", "worker", false},
	}
	for _, c := range cases {
		got := worktree.NeedsIsolation(c.purpose, c.role)
		if got != c.want {
			t.Errorf("NeedsIsolation(%q,%q) = %v, want %v", c.purpose, c.role, got, c.want)
		}
	}
}

// TestConcurrentWorkersCannotOverwriteEachOthersTrees is the 🎯T254.2
// acceptance oracle: two "workers" given their own worktree path (as
// jevons_agent_start would now compute) write to a same-named file inside
// their own tree at the same time; both survive with their own content, and
// the shared base repo's working tree is untouched.
func TestConcurrentWorkersCannotOverwriteEachOthersTrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	base := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", base}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "seed")

	agents := []string{"jv-worker-alpha", "jv-worker-beta"}
	paths := make([]string, len(agents))
	var wg sync.WaitGroup
	errs := make([]error, len(agents))
	for i, name := range agents {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			p, err := worktree.Ensure(base, name)
			paths[i] = p
			errs[i] = err
		}(i, name)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Ensure(%s): %v", agents[i], err)
		}
	}
	if paths[0] == paths[1] {
		t.Fatalf("concurrent workers got the same worktree path: %s", paths[0])
	}

	// Each worker writes into a same-named file inside its OWN tree.
	for i, p := range paths {
		content := []byte(agents[i] + " was here\n")
		if err := os.WriteFile(filepath.Join(p, "shared-name.txt"), content, 0o644); err != nil {
			t.Fatalf("write in %s: %v", p, err)
		}
	}
	// Neither overwrote the other's file, and the base repo's own tree never
	// saw either worker's file at all.
	for i, p := range paths {
		got, err := os.ReadFile(filepath.Join(p, "shared-name.txt"))
		if err != nil {
			t.Fatalf("read back %s: %v", p, err)
		}
		want := agents[i] + " was here\n"
		if string(got) != want {
			t.Fatalf("worker %s tree was clobbered: got %q want %q", agents[i], got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "shared-name.txt")); !os.IsNotExist(err) {
		t.Fatalf("base repo tree was polluted by a worker write: err=%v", err)
	}

	// Re-Ensure for an already-created worker is idempotent (resume case).
	p, err := worktree.Ensure(base, agents[0])
	if err != nil {
		t.Fatalf("re-Ensure: %v", err)
	}
	if p != paths[0] {
		t.Fatalf("re-Ensure returned a different path: %s vs %s", p, paths[0])
	}
}

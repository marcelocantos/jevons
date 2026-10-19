// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/targetfile"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// t991SharedClone builds a real git repo with a committed bullseye.yaml so a
// linked worktree carries its own tracked copy — the shape that made
// LedgerKey return two keys for one ledger before 🎯T991.
func t991SharedClone(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "jevons")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = base
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("git", "init", "-b", "master")
	run("git", "config", "user.email", "t991@test")
	run("git", "config", "user.name", "t991")
	ledger := "schema_version: 1\ntargets:\n  Tn:\n    name: worktree engagement\n    status: identified\n"
	if err := os.WriteFile(filepath.Join(base, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "README"), []byte("t991\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	return base
}

// 🎯T991: a 🎯T254.2 linked worktree resolves to the same LedgerKey as the
// shared clone it was made from. Pre-fix: DiscoverLedgerPath found the
// worktree's own tracked bullseye.yaml and engagement treated them as two
// ledgers.
func TestT991LedgerKeyCollapsesLinkedWorktreeOntoSharedClone(t *testing.T) {
	base := t991SharedClone(t)
	wt, err := worktree.Ensure(base, "jv-t991-worker")
	if err != nil {
		t.Fatal(err)
	}
	// Control: the worktree really does have its own bullseye.yaml on disk.
	// Without the rewrite, DiscoverLedgerPath would return that path.
	if _, err := os.Stat(filepath.Join(wt, "bullseye.yaml")); err != nil {
		t.Fatalf("worktree must carry its own bullseye.yaml copy: %v", err)
	}
	if path, err := targetfile.DiscoverLedgerPath(wt); err != nil || path != filepath.Join(wt, "bullseye.yaml") {
		t.Fatalf("DiscoverLedgerPath(worktree)=%q err=%v want worktree copy (control)", path, err)
	}

	sharedKey := targetfile.LedgerKey(base)
	wtKey := targetfile.LedgerKey(wt)
	if sharedKey == "" || wtKey == "" {
		t.Fatalf("empty key: shared=%q worktree=%q", sharedKey, wtKey)
	}
	if sharedKey != wtKey {
		t.Fatalf("LedgerKey(shared)=%q LedgerKey(worktree)=%q — must be one ledger", sharedKey, wtKey)
	}
	if !targetfile.SameLedgerAsWorkdirs(base, wt) {
		t.Fatal("SameLedgerAsWorkdirs(shared, worktree) must be true")
	}
	// Key is the shared clone's ledger path, not the worktree's copy.
	want := filepath.Join(base, "bullseye.yaml")
	if real, err := filepath.EvalSymlinks(want); err == nil {
		want = real
	}
	if sharedKey != want {
		t.Fatalf("LedgerKey(shared)=%q want %q", sharedKey, want)
	}
}

// 🎯T991 / 🎯T389 regression: two distinct repos still never merge, even when
// each has a linked worktree.
func TestT991LedgerKeyStillSeparatesTwoRepos(t *testing.T) {
	a := t991SharedClone(t)
	b := t991SharedClone(t)
	wtA, err := worktree.Ensure(a, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	wtB, err := worktree.Ensure(b, "worker-b")
	if err != nil {
		t.Fatal(err)
	}
	if targetfile.SameLedgerAsWorkdirs(wtA, wtB) {
		t.Fatalf("worktrees of two repos must not share a ledger key: %q vs %q",
			targetfile.LedgerKey(wtA), targetfile.LedgerKey(wtB))
	}
	if targetfile.SameLedgerAsWorkdirs(a, b) {
		t.Fatalf("two shared clones must not share a ledger key")
	}
}

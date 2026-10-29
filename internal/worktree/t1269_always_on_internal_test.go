// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// 🎯T1269: a branch that only adds or edits an unrelated package still runs
// both cross-cutting ratchets after the merge: scripts/docratchet (🎯T1197)
// and internal/testhome, which fails when a test package runs on the
// owner's HOME.
func TestT1269PostMergeAlwaysRunsTheTesthomeRatchet(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q", "-b", "master")
	for _, dir := range []string{"scripts/docratchet", "internal/testhome", "internal/newpkg"} {
		if err := os.MkdirAll(filepath.Join(repo, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, dir, "x.go"), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "seed")
	head := run("rev-parse", "HEAD")
	head = head[:len(head)-1]

	got := withAlwaysOnPostMergePackages(repo, head, []string{"internal/newpkg"})
	for _, want := range []string{"internal/newpkg", "scripts/docratchet", "internal/testhome"} {
		if !slices.Contains(got, want) {
			t.Fatalf("post-merge packages %v lack %s", got, want)
		}
	}
}

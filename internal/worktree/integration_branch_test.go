// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

func TestIntegrationBranchRecordAndCheck(t *testing.T) {
	base := t.TempDir()
	git(t, base, "init", "-q", "-b", "steer")
	git(t, base, "config", "user.email", "t@example.com")
	git(t, base, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, base, "add", "f")
	git(t, base, "commit", "-q", "-m", "init")

	wt, err := worktree.Ensure(base, "jv-t955-auto")
	if err != nil {
		t.Fatal(err)
	}
	got, err := worktree.ReadIntegrationBranch(wt)
	if err != nil {
		t.Fatal(err)
	}
	if got != "steer" {
		t.Fatalf("recorded branch %q, want steer", got)
	}
	if err := worktree.CheckIntegrationBranch(wt, "steer"); err != nil {
		t.Fatal(err)
	}
	if err := worktree.CheckIntegrationBranch(wt, "master"); err == nil {
		t.Fatal("expected mismatch error")
	}
}

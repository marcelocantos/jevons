// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

// 🎯T1144: git skips pre-commit on a conflict-free merge; pre-merge-commit must
// still refuse a bullseye.yaml that shrank versus either parent.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fullMergeLedger = `schema_version: 1
targets:
  T1: {name: a}
  T2: {name: b}
  T3: {name: c}
  T4: {name: d}
  T5: {name: e}
  T6: {name: f}
  T7: {name: g}
  T8: {name: h}
`

const shrunkMergeLedger = `schema_version: 1
targets:
  T1: {name: a}
`

func TestGitMergeRefusesTruncatedLedgerBranch(t *testing.T) {
	repo := newLedgerMergeRepo(t)

	if out, err := repo.git(t, "checkout", "-b", "jv-shrink"); err != nil {
		t.Fatalf("checkout branch: %v\n%s", err, out)
	}
	repo.write(t, "bullseye.yaml", shrunkMergeLedger)
	if out, err := repo.git(t, "add", "bullseye.yaml"); err != nil {
		t.Fatalf("stage shrink: %v\n%s", err, out)
	}
	if out, err := repo.git(t, "commit", "-q", "--no-verify", "-m", "evil shrink"); err != nil {
		t.Fatalf("seed evil branch (no-verify): %v\n%s", err, out)
	}

	if out, err := repo.git(t, "checkout", "master"); err != nil {
		t.Fatalf("checkout master: %v\n%s", err, out)
	}
	repo.write(t, "keep.txt", "master-only\n")
	if out, err := repo.git(t, "add", "keep.txt"); err != nil {
		t.Fatalf("stage keep: %v\n%s", err, out)
	}
	if out, err := repo.git(t, "commit", "-q", "--no-verify", "-m", "diverge master"); err != nil {
		t.Fatalf("master diverge commit: %v\n%s", err, out)
	}
	base := repo.head(t)
	out, err := repo.gitEnv(t, []string{"JEVONS_COMMIT_SCOPE=off"}, "merge", "--no-edit", "jv-shrink")
	if err == nil {
		t.Fatalf("merge should be refused; landed:\n%s", repo.showStat(t, "HEAD"))
	}
	if !strings.Contains(out, "bullseye.yaml") || !strings.Contains(out, "Lost target rows") {
		t.Fatalf("merge refusal should name ledger shrink:\n%s", out)
	}
	if got := repo.head(t); got != base {
		t.Fatalf("merge moved HEAD %s -> %s", base, got)
	}
	out, err = repo.git(t, "show", "HEAD:bullseye.yaml")
	if err != nil {
		t.Fatalf("read HEAD ledger: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(fullMergeLedger) {
		t.Fatalf("refused merge must leave master ledger intact:\n%s", out)
	}
}

func newLedgerMergeRepo(t *testing.T) *guardedRepo {
	t.Helper()
	if _, err := os.Stat(filepath.Join(repoRoot(t), "scripts", "hooks", "pre-merge-commit")); err != nil {
		t.Fatalf("shipped pre-merge-commit hook: %v", err)
	}
	r := &guardedRepo{dir: t.TempDir(), bin: guardBinary(t)}

	if out, err := r.git(t, "init", "-q", "-b", "master", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, kv := range [][2]string{{"user.email", "t@example.invalid"}, {"user.name", "t"}} {
		if out, err := r.git(t, "config", kv[0], kv[1]); err != nil {
			t.Fatalf("git config: %v\n%s", err, out)
		}
	}
	installShippedHook(t, r.dir, "pre-commit")
	installShippedHook(t, r.dir, "pre-merge-commit")

	r.write(t, "bullseye.yaml", fullMergeLedger)
	if out, err := r.git(t, "add", "bullseye.yaml"); err != nil {
		t.Fatalf("seed add: %v\n%s", err, out)
	}
	if out, err := r.git(t, "commit", "-q", "--no-verify", "-m", "seed full ledger"); err != nil {
		t.Fatalf("seed commit: %v\n%s", err, out)
	}
	return r
}

func installShippedHook(t *testing.T, repoDir, hookName string) {
	t.Helper()
	hook := filepath.Join(repoDir, ".git", "hooks", hookName)
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	shim, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "hooks", hookName))
	if err != nil {
		t.Fatalf("reading shipped %s: %v", hookName, err)
	}
	if err := os.WriteFile(hook, shim, 0o755); err != nil {
		t.Fatal(err)
	}
}

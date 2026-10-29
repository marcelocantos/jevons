// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ledgercommit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/ledgercommit"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestCommitDirtyLedgerScopedOnly(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "master")
	git(t, dir, "config", "user.email", "t@test")
	git(t, dir, "config", "user.name", "t")
	ledger := filepath.Join(dir, "bullseye.yaml")
	if err := os.WriteFile(ledger, []byte("schema_version: 1\ntargets: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "bullseye.yaml")
	git(t, dir, "commit", "-q", "-m", "init")
	if err := os.WriteFile(ledger, []byte("schema_version: 1\ntargets:\n  T1147: {status: achieved}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ledgercommit.CommitDirtyLedger(dir, "achieve T1147"); err != nil {
		t.Fatal(err)
	}
	stat := string(mustGit(t, dir, "show", "--stat", "--oneline", "HEAD"))
	if !strings.Contains(stat, "bullseye.yaml") {
		t.Fatalf("commit did not include ledger: %s", stat)
	}
	if strings.Contains(stat, "other.txt") {
		t.Fatalf("commit swept foreign path: %s", stat)
	}
	st, _ := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if !strings.Contains(string(st), "other.txt") {
		t.Fatalf("expected other.txt still dirty, got %q", st)
	}
}

func mustGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func TestWriteMergedLedgerPreservesWorkingAchieve(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "master")
	head := []byte("schema_version: 1\ntargets:\n  T1: {name: a}\n")
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), head, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "init")
	working := []byte("schema_version: 1\ntargets:\n  T1: {name: a}\n  T1147: {status: achieved}\n")
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), working, 0o644); err != nil {
		t.Fatal(err)
	}
	merged, err := ledgercommit.WriteMergedLedger(dir, head, working, head)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), "T1147") {
		t.Fatalf("merge restore dropped row: %s", merged)
	}
}

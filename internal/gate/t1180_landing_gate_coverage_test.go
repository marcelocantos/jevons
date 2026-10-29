// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitBranchGoPackageDirs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "internal/a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "internal/b"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, "go.mod"), "module example.com/x\n\ngo 1.24\n")
	write(filepath.Join(repo, "internal/a/a.go"), "package a\n")
	write(filepath.Join(repo, "internal/b/b.go"), "package b\n")
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "master")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	git("add", ".")
	git("commit", "-q", "-m", "seed")
	base := strings.TrimSpace(mustGitOut(t, repo, "rev-parse", "master"))
	write(filepath.Join(repo, "internal/b/b.go"), "package b\n\nfunc B() {}\n")
	git("commit", "-q", "-am", "change b")
	tip := strings.TrimSpace(mustGitOut(t, repo, "rev-parse", "HEAD"))

	got, err := GitBranchGoPackageDirs(repo, base, tip)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "internal/b" {
		t.Fatalf("got %v, want [internal/b]", got)
	}
}

func mustGitOut(t *testing.T, repo string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

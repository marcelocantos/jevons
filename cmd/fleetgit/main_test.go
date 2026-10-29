// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/treeguard"
	"github.com/marcelocantos/jevons/internal/worktree"
)

func TestFleetgitRefusesStashInSharedClone(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "master")
	git(t, root, "config", "user.email", "t@example.com")
	git(t, root, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "dirty.txt")
	git(t, root, "commit", "-q", "-m", "seed")
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fleetgitBin := filepath.Join(bin, "fleetgit")
	if out, err := exec.Command("go", "build", "-o", fleetgitBin, ".").Output(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	forms := [][]string{
		{"stash"},
		{"stash", "-m", "test"},
		{"stash", "push", "-m", "test"},
		{"stash", "save", "legacy"},
		{"stash", "pop"},
		{"stash", "apply"},
	}
	for _, args := range forms {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := exec.Command(fleetgitBin, args...)
			cmd.Dir = root
			cmd.Env = append(os.Environ(),
				commitattrib.ActorEnv+"=jv-t1149-auto",
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			)
			combined, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected refusal, got success: %s", combined)
			}
			if !strings.Contains(string(combined), "dirty.txt") {
				t.Fatalf("refusal should name dirty path: %s", combined)
			}
		})
	}
}

// 🎯T1151.1: fleetgit must refuse mutating git against the shared clone from a
// linked worktree seat (production path through main, not only fleetgit.Refuse*).
func TestFleetgitRefusesGitCommitInSharedClone(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "jevons")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, base, "init", "-q", "-b", "master")
	git(t, base, "config", "user.email", "t@example.com")
	git(t, base, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, base, "add", ".")
	git(t, base, "commit", "-q", "-m", "seed")

	wt, err := worktree.Ensure(base, "jv-t1151-seat")
	if err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fleetgitBin := filepath.Join(bin, "fleetgit")
	if out, err := exec.Command("go", "build", "-o", fleetgitBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(fleetgitBin, "-C", base, "commit", "-m", "must-not-land")
	cmd.Dir = wt
	cmd.Env = append(os.Environ(),
		commitattrib.ActorEnv+"=jv-t1151-seat",
		treeguard.ProjectDirEnv+"="+wt,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	combined, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected refusal, got success: %s", combined)
	}
	var exit int
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	}
	if exit != 2 {
		t.Fatalf("expected exit 2, got %d: %s", exit, combined)
	}
	if !strings.Contains(string(combined), "jv-t1151-seat") || !strings.Contains(string(combined), "commit") {
		t.Fatalf("refusal should name seat and command: %s", combined)
	}
}

// 🎯T1151.4: fleetgit refuses reset --hard against the shared clone from a
// linked worktree seat.
func TestFleetgitRefusesGitResetHardInSharedClone(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "jevons")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, base, "init", "-q", "-b", "master")
	git(t, base, "config", "user.email", "t@example.com")
	git(t, base, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, base, "add", ".")
	git(t, base, "commit", "-q", "-m", "seed")
	wt, err := worktree.Ensure(base, "jv-t1151.4-seat")
	if err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fleetgitBin := filepath.Join(bin, "fleetgit")
	if out, err := exec.Command("go", "build", "-o", fleetgitBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(fleetgitBin, "-C", base, "reset", "--hard")
	cmd.Dir = wt
	cmd.Env = append(os.Environ(),
		commitattrib.ActorEnv+"=jv-t1151.4-seat",
		treeguard.ProjectDirEnv+"="+wt,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	combined, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected refusal, got success: %s", combined)
	}
	if !strings.Contains(string(combined), "reset --hard") || !strings.Contains(string(combined), "jv-t1151.4-seat") {
		t.Fatalf("refusal should name seat and command: %s", combined)
	}
}

// 🎯T1151.6: fleetgit must refuse mutating git against a sibling repo's shared clone.
func TestFleetgitRefusesGitCommitInSiblingSharedClone(t *testing.T) {
	org := t.TempDir()
	jevons := filepath.Join(org, "jevons")
	claudia := filepath.Join(org, "claudia")
	for _, base := range []string{jevons, claudia} {
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, base, "init", "-q", "-b", "master")
		git(t, base, "config", "user.email", "t@example.com")
		git(t, base, "config", "user.name", "t")
		if err := os.WriteFile(filepath.Join(base, "seed.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, base, "add", ".")
		git(t, base, "commit", "-q", "-m", "seed")
	}
	wt, err := worktree.Ensure(jevons, "jv-t1151.6-prod")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fleetgitBin := filepath.Join(bin, "fleetgit")
	if out, err := exec.Command("go", "build", "-o", fleetgitBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(fleetgitBin, "-C", claudia, "commit", "-m", "must-not-land")
	cmd.Dir = wt
	cmd.Env = append(os.Environ(),
		commitattrib.ActorEnv+"=jv-t1151.6-prod",
		treeguard.ProjectDirEnv+"="+wt,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	combined, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected refusal, got success: %s", combined)
	}
	if !strings.Contains(string(combined), "claudia-po") {
		t.Fatalf("refusal should name sibling PO: %s", combined)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

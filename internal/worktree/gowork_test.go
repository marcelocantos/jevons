// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// An isolated tree sits beside the shared clone, under the org-level go.work
// that lists the clone but not the tree. MirrorGoWork must give the tree a
// workspace that builds it, with the same unpublished siblings, and keep that
// file out of every commit.
func TestMirrorGoWorkBuildsTheTreeWithSiblings(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	t.Setenv("GOWORK", "")
	t.Setenv("GOFLAGS", "")
	org := t.TempDir()
	base := filepath.Join(org, "app")
	sib := filepath.Join(org, "lib")
	writeFile(t, filepath.Join(org, "go.work"), "go 1.24\n\nuse (\n\t./app\n\t./lib\n)\n")
	writeFile(t, filepath.Join(sib, "go.mod"), "module example.com/lib\n\ngo 1.24\n")
	writeFile(t, filepath.Join(sib, "lib.go"), "package lib\n\nconst Name = \"lib\"\n")
	writeFile(t, filepath.Join(base, "go.mod"), "module example.com/app\n\ngo 1.24\n\nrequire example.com/lib v0.0.0\n")
	writeFile(t, filepath.Join(base, "main.go"), "package main\n\nimport \"example.com/lib\"\n\nfunc main() { println(lib.Name) }\n")
	git(t, base, "init", "-q", "-b", "master")
	git(t, base, "config", "user.email", "t@example.com")
	git(t, base, "config", "user.name", "t")
	git(t, base, "add", ".")
	git(t, base, "commit", "-q", "-m", "seed")

	wt, err := worktree.Ensure(base, "jv-alpha")
	if err != nil {
		t.Fatal(err)
	}
	// The failure being fixed: the tree is governed by org/go.work, which
	// does not list it.
	if out, err := goIn(wt, "build", "-o", os.DevNull, "./..."); err == nil {
		t.Fatalf("precondition: expected the unmirrored tree to fail to build, got success: %s", out)
	}

	wrote, err := worktree.MirrorGoWork(base, wt)
	if err != nil || !wrote {
		t.Fatalf("MirrorGoWork = %v, %v; want true, nil", wrote, err)
	}
	work := readFile(t, filepath.Join(wt, "go.work"))
	if !strings.Contains(work, "go 1.24") || !strings.Contains(work, "\t.\n") || !strings.Contains(work, "\t"+sib+"\n") {
		t.Fatalf("mirrored go.work lacks the version, the tree, or the sibling:\n%s", work)
	}
	if strings.Contains(work, base+"\n") {
		t.Fatalf("mirrored go.work still uses the shared clone instead of the tree:\n%s", work)
	}
	if out, err := goIn(wt, "build", "-o", os.DevNull, "./..."); err != nil {
		t.Fatalf("tree does not build with its mirrored go.work: %v: %s", err, out)
	}
	if st := git(t, wt, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Fatalf("go.work is not excluded from git in the tree:\n%s", st)
	}

	// Idempotent: a second call leaves the existing file alone.
	if wrote, err := worktree.MirrorGoWork(base, wt); err != nil || wrote {
		t.Fatalf("second MirrorGoWork = %v, %v; want false, nil", wrote, err)
	}
}

func TestMirrorGoWorkWithoutWorkspaceIsNoop(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	t.Setenv("GOWORK", "off")
	base := sharedClone(t)
	wt, err := worktree.Ensure(base, "jv-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if wrote, err := worktree.MirrorGoWork(base, wt); err != nil || wrote {
		t.Fatalf("MirrorGoWork with no workspace = %v, %v; want false, nil", wrote, err)
	}
}

func goIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

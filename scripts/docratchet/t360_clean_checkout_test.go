// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T360: a clean checkout of HEAD builds with no generator step. Every
// //go:embed input must be tracked — a gitignored one (internal/cli/help_agent.md
// was) makes a pristine clone, CI runner, or release build fail with
// "pattern …: no matching files found" instead of naming the missing
// `make` step.
package docratchet_test

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// gitRepo reports whether repoRoot is inside a git work tree, skipping the
// test otherwise (source tarballs have no HEAD to check out).
func gitRepo(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Skip("not a git work tree")
	}
	return root
}

// TestT360CleanCheckoutBuilds is the load-bearing ratchet: a detached
// worktree of HEAD in a temp dir must `go build ./...` green with no prior
// `make`. It tests the *committed* tree, so uncommitted generator output in
// the working copy cannot mask a broken checkout.
func TestT360CleanCheckoutBuilds(t *testing.T) {
	root := gitRepo(t)

	// git refuses a non-empty target, so name a child of the temp dir.
	wt := filepath.Join(t.TempDir(), "head")
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "--detach", wt, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	// 🎯T440: stamp the owner so the sweeper can reap this tree when the
	// cleanup below does not run. It did not run three times: a timeout
	// panic, a SIGKILL and a dropped session each skip t.Cleanup entirely,
	// and the leaked directories still exist, so `git worktree prune`
	// considers them healthy. The Mark is the half of the lifetime that
	// survives this process; the sweep decides the rest from outside.
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatalf("mark worktree owner: %v", err)
	}
	t.Cleanup(func() {
		// TempDir removal leaves .git/worktrees metadata behind; prune it.
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt).Run()
		_ = exec.Command("git", "-C", root, "worktree", "prune").Run()
	})

	build := exec.Command("go", "build", "./...")
	build.Dir = wt
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("clean checkout of HEAD does not build (%v).\n"+
			"A pristine clone must not need a `make` generator step first.\n%s", err, out)
	}
}

// TestT360NoGitignoredEmbedInputs is the fast general form of the same
// invariant: no //go:embed pattern may resolve to a gitignored path. It
// fails immediately on the next generated-and-ignored embed input, without
// waiting for the full worktree build.
func TestT360NoGitignoredEmbedInputs(t *testing.T) {
	root := gitRepo(t)

	var paths []string
	skipDir := map[string]bool{
		".git": true, "bin": true, "build": true, ".build": true,
		"node_modules": true, "DerivedData": true,
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		paths = append(paths, embedInputs(t, path)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("found no //go:embed inputs — scanner is broken, not the repo")
	}

	// git check-ignore exits 1 when nothing is ignored; any stdout is a hit.
	check := exec.Command("git", "-C", root, "check-ignore", "--stdin")
	check.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	out, _ := check.Output()
	if ignored := strings.TrimSpace(string(out)); ignored != "" {
		t.Errorf("//go:embed inputs are gitignored, so a clean checkout cannot build:\n%s\n"+
			"Commit these files (or stop embedding them) — see 🎯T360.", ignored)
	}
}

// embedInputs returns the files every //go:embed directive in a Go file
// resolves to, and fails the test if a pattern matches nothing in the
// working tree (the same symptom T360 fixed, seen from the dirty side).
func embedInputs(t *testing.T, goFile string) []string {
	t.Helper()
	f, err := os.Open(goFile)
	if err != nil {
		t.Fatalf("open %s: %v", goFile, err)
	}
	defer f.Close()

	var out []string
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if !strings.HasPrefix(line, "//go:embed ") {
			continue
		}
		for pat := range strings.FieldsSeq(strings.TrimPrefix(line, "//go:embed ")) {
			// `all:` opts hidden/underscore files into a directory embed.
			pat = strings.TrimPrefix(strings.Trim(pat, `"`), "all:")
			matches, err := filepath.Glob(filepath.Join(filepath.Dir(goFile), pat))
			if err != nil {
				t.Fatalf("%s: bad embed pattern %q: %v", goFile, pat, err)
			}
			if len(matches) == 0 {
				t.Errorf("%s: //go:embed %s matches no files in the working tree", goFile, pat)
				continue
			}
			out = append(out, matches...)
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan %s: %v", goFile, err)
	}
	return out
}

// TestT360EmbeddedGuideMirrorsAgentsGuide guards the cost of committing the
// mirror: internal/cli/help_agent.md is now tracked, so it can drift from
// its source. The Makefile copies it; this fails if a commit forgot to.
func TestT360EmbeddedGuideMirrorsAgentsGuide(t *testing.T) {
	if src, mirror := readRepo(t, "agents-guide.md"), readRepo(t, "internal/cli/help_agent.md"); src != mirror {
		t.Errorf("internal/cli/help_agent.md has drifted from agents-guide.md; " +
			"run `make internal/cli/help_agent.md` and commit the result")
	}
}

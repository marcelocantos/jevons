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

// 🎯T254.2: a worker's isolated tree lives at ../.jevons-worktrees-<repo>/<name>,
// so the checkout's own parent holds no sibling modules. `gate -clean` run from
// that tree must still find claudia next to the shared clone, or every clean
// gate a worker runs builds against the published pin and fails.
//
// 🎯T440 //worktreereap:exempt — the worktree lives, with the repo it belongs to, inside
// t.TempDir: no shared clone ever lists it, so there is nothing for the 🎯T440
// sweeper to reap even if this test dies before cleanup.
func TestInjectCleanSiblingGoWorkFromIsolatedTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	// git answers with symlinks resolved (/var -> /private/var on macOS).
	org, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(org, "jevons")
	writeGoMod(t, clone, "github.com/marcelocantos/jevons")
	writeGoMod(t, filepath.Join(org, "claudia"), "github.com/marcelocantos/claudia")
	git := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git(clone, "init", "-q", "-b", "master")
	git(clone, "-c", "user.email=t@example.com", "-c", "user.name=t", "add", ".")
	git(clone, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "seed")
	isolated := filepath.Join(org, ".jevons-worktrees-jevons", "jv-worker")
	git(clone, "worktree", "add", "-q", "-b", "jevons-worktree/jv-worker", isolated)

	wt := filepath.Join(t.TempDir(), "tree")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	restore := injectCleanSiblingGoWork(isolated, wt)
	defer restore()
	data, err := os.ReadFile(filepath.Join(wt, "go.work"))
	if err != nil {
		t.Fatalf("no sibling go.work written for a gate run from an isolated tree: %v", err)
	}
	if !strings.Contains(string(data), "marcelocantos/claudia => "+filepath.Join(org, "claudia")) {
		t.Fatalf("go.work does not replace claudia with the shared clone's sibling:\n%s", data)
	}
}

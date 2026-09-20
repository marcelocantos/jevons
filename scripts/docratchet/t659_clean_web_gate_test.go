// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T659: the clean-checkout web gate is one make target that owns its
// worktree's dependencies, so removing the worktree can never empty the
// shared clone's ui/node_modules.
//
// On 2026-09-15 a worker ran the 🎯T398 gate by hand in a detached worktree
// whose ui/node_modules was a symlink into the shared clone. The checkout's
// lockfile was newer than the linked vitest, so `make ui-deps` re-ran
// `npm ci`, which empties node_modules in place — through the link. The
// shared install (171 packages) was gone twice in one slice, and every
// neighbour running vitest at the time would have gone red for it. T438 and
// T563 forbid a symlinked node_modules for their own oracles; nothing owned
// the recipe workers actually ran. Now `make test-web-clean` does, and this
// file pins the three halves: the removal path spares a linked target, the
// Makefile carries the target, and the instructions name it.
package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// TestT659RemovalPathSparesSymlinkedNodeModules builds a real detached
// worktree of HEAD, links its ui/node_modules at a stand-in for the shared
// install, runs the recipe's removal path, and checks the stand-in still
// holds vitest and vite. The stand-in, not the clone's own ui/node_modules,
// is the target on purpose: a ratchet that reproduces the incident on the
// real directory when it goes red would be the incident.
func TestT659RemovalPathSparesSymlinkedNodeModules(t *testing.T) {
	root := gitRepo(t)
	scratch := t.TempDir()
	shared := filepath.Join(scratch, "shared-clone", "ui", "node_modules")
	for _, pkg := range []string{"vitest", "vite"} {
		if err := os.MkdirAll(filepath.Join(shared, pkg), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shared, pkg, "package.json"), []byte(`{"name":"`+pkg+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	wt := filepath.Join(scratch, "head")
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "--detach", wt, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: wt, Note: t.Name()}); err != nil {
		t.Fatalf("mark worktree owner: %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", wt).Run()
		_ = exec.Command("git", "-C", root, "worktree", "prune").Run()
	})
	link := filepath.Join(wt, "ui", "node_modules")
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(link, "vitest", "package.json")); err != nil {
		t.Fatalf("the link does not reach the stand-in shared install: %v", err)
	}

	if err := gate.RemoveWorktree(root, wt); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Lstat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree %s still on disk after removal: %v", wt, err)
	}
	for _, pkg := range []string{"vitest", "vite"} {
		if _, err := os.Stat(filepath.Join(shared, pkg, "package.json")); err != nil {
			t.Fatalf("removing the worktree reached through the ui/node_modules symlink and took %s with it (🎯T659): %v", pkg, err)
		}
	}
}

// TestT659MakefileOwnsTheCleanWebGate pins the recipe: a test-web-clean
// target that runs scripts/test-web-clean, accepts SHA, and never links.
func TestT659MakefileOwnsTheCleanWebGate(t *testing.T) {
	mk := readRepo(t, "Makefile")
	i := strings.Index(mk, "\ntest-web-clean:")
	if i < 0 {
		t.Fatalf("Makefile has no test-web-clean target (🎯T659)")
	}
	recipe := mk[i:]
	if j := strings.Index(recipe[1:], "\n\n"); j >= 0 {
		recipe = recipe[:j+1]
	}
	if !strings.Contains(recipe, "./scripts/test-web-clean") {
		t.Fatalf("test-web-clean must run ./scripts/test-web-clean; recipe:\n%s", recipe)
	}
	if !strings.Contains(recipe, "$(SHA)") {
		t.Fatalf("test-web-clean must accept SHA=<commit>; recipe:\n%s", recipe)
	}
	if strings.Contains(recipe, "ln -s") {
		t.Fatalf("test-web-clean must not symlink anything into the worktree; recipe:\n%s", recipe)
	}
}

// TestT659InstructionsNameTheRecipe: the 🎯T398 guidance in AGENTS.md and
// agents-guide.md sends workers to make test-web-clean and names the
// symlinked node_modules as the failure. The help_agent.md mirror is
// ratcheted by T360.
func TestT659InstructionsNameTheRecipe(t *testing.T) {
	// A bullet in AGENTS.md, a section in agents-guide.md: the guidance is
	// whatever follows a T398 mention within one screen of text.
	const window = 2500
	for _, rel := range []string{"AGENTS.md", "agents-guide.md"} {
		doc := readRepo(t, rel)
		var found bool
		for _, loc := range regexp.MustCompile(`T398`).FindAllStringIndex(doc, -1) {
			block := doc[loc[0]:min(len(doc), loc[0]+window)]
			if strings.Contains(block, "make test-web-clean") && strings.Contains(block, "symlink") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: the 🎯T398 guidance must name `make test-web-clean` as the recipe and the symlinked ui/node_modules as the failure (🎯T659)", rel)
		}
	}
}

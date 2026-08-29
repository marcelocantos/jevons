// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T563: `make test-ui-react` used to print "skip test-ui-react (no
// ui/node_modules)" and exit 0, so `bin/gate -clean -- make test-ui-react`
// recorded GREEN in ~100ms over a vitest suite that never ran (e43fce04),
// while the same gate after `npm ci` took the real time (f74a9f87). A skip
// that returns 0 is a false green — same family as 🎯T438 (playwright deps).
//
// The fix is that the Makefile installs ui deps when absent and never skips.
// This ratchet pins both halves: the recipe text has no skip branch, and a
// detached worktree of HEAD (no ui/node_modules) ends up with a require-able
// vitest after `make ui-deps`.
package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktreereap"
)

func TestT563MakefileTestUIReactNeverSkips(t *testing.T) {
	root := gitRepo(t)
	b, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(b)
	if strings.Contains(mk, "skip test-ui-react") {
		t.Fatalf("Makefile still carries the 🎯T563 hollow skip (\"skip test-ui-react\"): a missing ui/node_modules must install or fail, never exit 0")
	}
	if !strings.Contains(mk, "test-ui-react: ui-deps") {
		t.Fatalf("Makefile: test-ui-react must depend on ui-deps so a clean checkout installs vitest before running")
	}
}

func TestT563CleanCheckoutUIReactDepsInstall(t *testing.T) {
	root := gitRepo(t)
	for _, bin := range []string{"node", "npm", "make"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	if testing.Short() {
		t.Skip("npm ci in a fresh worktree is not a -short oracle")
	}
	wt := filepath.Join(t.TempDir(), "head")
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
	if _, err := os.Stat(filepath.Join(wt, "ui", "node_modules")); err == nil {
		t.Fatalf("fresh worktree already has ui/node_modules; the oracle is not testing a clean checkout")
	}
	install := exec.Command("make", "ui-deps")
	install.Dir = wt
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("`make ui-deps` failed in a clean checkout of HEAD (%v).\n%s", err, out)
	}
	vitest := filepath.Join(wt, "ui", "node_modules", "vitest")
	req := exec.Command("node", "-e", "require.resolve("+jsString(vitest)+")")
	req.Dir = wt
	if out, err := req.CombinedOutput(); err != nil {
		t.Fatalf("vitest not require-able after make ui-deps (%v).\n%s", err, out)
	}
}

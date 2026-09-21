// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T438: the Playwright UI suite must be invocable on the *committed* tree,
// not merely in the shared clone where scripts/browser-loop-test/node_modules
// already exists as someone's hand-install (or a symlink into it — the
// contamination that let 🎯T370's smoke go green while a fresh worktree died
// with MODULE_NOT_FOUND). Same family as 🎯T360 (gitignored embed input) and
// 🎯T398 (shared-clone web green ≠ master green).
//
// node_modules is gitignored on purpose; the fix is that `make test-ui`
// installs when absent. This ratchet checks that a detached worktree of HEAD
// can require playwright after the build's install step, and runs one fast
// UI oracle; a missing dependency fails rather than claiming
// green over MODULE_NOT_FOUND.
package docratchet_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// TestT438CleanCheckoutUISuiteInvocable checks HEAD out into a detached
// worktree and asserts the Playwright UI path is invocable there: make
// installs the gitignored deps, require(playwright) succeeds, and one UI
// test exits 0 — never
// MODULE_NOT_FOUND.
func TestT438CleanCheckoutUISuiteInvocable(t *testing.T) {
	root := gitRepo(t)
	for _, bin := range []string{"node", "npm", "make"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("required UI dependency %s not on PATH", bin)
		}
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

	// Build's job: install when absent. A clean tree must not need a
	// hand-copied node_modules or a symlink into the shared clone.
	install := exec.Command("make", "playwright-deps")
	install.Dir = wt
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("`make playwright-deps` failed in a clean checkout of HEAD (%v).\n"+
			"A pristine worktree must install playwright without caller memory.\n%s",
			err, out)
	}

	playwrightMod := filepath.Join(wt, "scripts", "browser-loop-test", "node_modules", "playwright")
	requireCmd := exec.Command("node", "-e", "require("+jsString(playwrightMod)+")")
	requireCmd.Dir = wt
	if out, err := requireCmd.CombinedOutput(); err != nil {
		msg := string(out)
		if strings.Contains(msg, "MODULE_NOT_FOUND") || strings.Contains(err.Error(), "MODULE_NOT_FOUND") {
			t.Fatalf("playwright still MODULE_NOT_FOUND after make playwright-deps (%v).\n"+
				"The install step did not leave a require-able module at %s.\n%s",
				err, playwrightMod, out)
		}
		t.Fatalf("require(playwright) failed after install (%v).\n%s", err, out)
	}

	if testing.Short() {
		// Install + require is the load-bearing half of 🎯T438; browser
		// launch is gated so the ratchet stays cheap under -short.
		return
	}

	// One UI oracle, not the whole `make test-ui`. What 🎯T438 guards is that
	// the Playwright path is invocable on a clean checkout: the UI build the
	// suites need, the browser install, and one suite that launches a
	// browser. The remaining `make test-ui` scripts (legacy-obligations,
	// t789, t799) assert product behaviour, not invocability, and already
	// run as their own step of `make test`; repeating them here made this
	// one test the longest in the package (over 10 min at host load ~200,
	// past Go's default -timeout, while the same package passed with
	// -timeout 45m — 🎯T803). The cost that remains is dependency install
	// and build, which is what this test exists to exercise.
	for _, step := range [][]string{
		{"make", "ui-build", "playwright-browser"},
		{"node", "scripts/react-ui-test/test.cjs"},
	} {
		ui := exec.Command(step[0], step[1:]...)
		ui.Dir = wt
		out, err := ui.CombinedOutput()
		if err == nil {
			continue
		}
		msg := string(out)
		if strings.Contains(msg, "MODULE_NOT_FOUND") {
			t.Fatalf("`%s` died with MODULE_NOT_FOUND in a clean checkout (%v).\n"+
				"Install must make playwright require-able; this is not a browser-binary skip.\n%s",
				strings.Join(step, " "), err, out)
		}
		t.Fatalf("`%s` RED on clean HEAD (%v).\n%s", strings.Join(step, " "), err, out)
	}
}

func jsString(s string) string {
	b := strings.Builder{}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

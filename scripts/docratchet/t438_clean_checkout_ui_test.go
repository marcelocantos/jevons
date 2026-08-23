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
// UI oracle so a missing browser binary skips by name rather than claiming
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
// test either exits 0 or skips naming a missing browser binary — never
// MODULE_NOT_FOUND.
func TestT438CleanCheckoutUISuiteInvocable(t *testing.T) {
	root := gitRepo(t)
	for _, bin := range []string{"node", "npm", "make"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
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

	// One fast UI oracle named in the acceptance (not the full make test-ui).
	ui := exec.Command("node", "scripts/chat-ui-test/t370-fleet-cycle-test.js")
	ui.Dir = wt
	out, err := ui.CombinedOutput()
	if err == nil {
		return
	}
	msg := string(out)
	if strings.Contains(msg, "MODULE_NOT_FOUND") {
		t.Fatalf("UI oracle died with MODULE_NOT_FOUND in a clean checkout (%v).\n"+
			"Install must make playwright require-able; this is not a browser-binary skip.\n%s",
			err, out)
	}
	if missingBrowser(msg) {
		t.Skipf("playwright browser binary missing (deps installed; MODULE_NOT_FOUND ruled out):\n%s", msg)
	}
	t.Fatalf("`node scripts/chat-ui-test/t370-fleet-cycle-test.js` RED on clean HEAD (%v).\n%s", err, out)
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

func missingBrowser(msg string) bool {
	needles := []string{
		"Executable doesn't exist",
		"browserType.launch",
		"Please run the following command to download new browsers",
		"npx playwright install",
		"playwright install",
	}
	lower := strings.ToLower(msg)
	for _, n := range needles {
		if strings.Contains(msg, n) || strings.Contains(lower, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope_test

// Oracle for the install half of 🎯T377: a clone nobody set up by hand must
// still refuse the sweeping commit. The end-to-end test below is the one that
// matters — it takes a repository with no hook at all, runs what `make` runs,
// and then replays the incident against it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitscope"
)

func TestClassifyExistingOnlyReplacesOurOwnHook(t *testing.T) {
	ours := []byte("#!/usr/bin/env bash\n# guard (🎯T377)\nexec bin/commitscope\n")
	for _, c := range []struct {
		name     string
		existing string
		want     commitscope.InstallOutcome
	}{
		{"identical", string(ours), commitscope.HookCurrent},
		{"older copy of ours", "#!/bin/sh\n# guard 🎯T377 v1\nexit 0\n", commitscope.HookUpdated},
		{"somebody else's hook", "#!/bin/sh\nexec make lint\n", commitscope.HookForeign},
		{"empty file", "", commitscope.HookForeign},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := commitscope.ClassifyExisting([]byte(c.existing), ours); got != c.want {
				t.Errorf("ClassifyExisting = %v, want %v", got, c.want)
			}
		})
	}
}

// TestInstallRefusesToSpeakForAHookItDidNotWrite is the destructive case: a
// build step that silently overwrote a developer's own pre-commit hook would
// be doing the very thing this target is about.
func TestInstallRefusesToSpeakForAHookItDidNotWrite(t *testing.T) {
	hooks := t.TempDir()
	foreign := "#!/bin/sh\nexec make lint\n"
	dest := filepath.Join(hooks, "pre-commit")
	if err := os.WriteFile(dest, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}

	outcome, err := commitscope.InstallHook(hooks, shimPath(t))
	if err != nil {
		t.Fatalf("InstallHook: %v", err)
	}
	if outcome != commitscope.HookForeign {
		t.Errorf("outcome = %v, want %v", outcome, commitscope.HookForeign)
	}
	if outcome.Active() {
		t.Error("a clone running somebody else's hook was reported as guarded")
	}
	if got := readFile(t, dest); got != foreign {
		t.Errorf("the foreign hook was modified:\n%s", got)
	}
	if report := commitscope.InstallReport(outcome, hooks); !strings.Contains(report, "NOT active") {
		t.Errorf("report does not say the guard is inactive:\n%s", report)
	}
}

func TestInstallIsIdempotentAndRefreshesItsOwnOlderCopy(t *testing.T) {
	hooks := t.TempDir()

	if outcome, err := commitscope.InstallHook(hooks, shimPath(t)); err != nil || outcome != commitscope.HookInstalled {
		t.Fatalf("first install = %v, %v; want %v", outcome, err, commitscope.HookInstalled)
	}
	dest := filepath.Join(hooks, "pre-commit")
	shim := readFile(t, shimPath(t))
	if got := readFile(t, dest); got != shim {
		t.Errorf("installed hook differs from the shipped shim:\n%s", got)
	}
	if info, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed hook is not executable (mode %v); git would ignore it", info.Mode())
	}

	if outcome, err := commitscope.InstallHook(hooks, shimPath(t)); err != nil || outcome != commitscope.HookCurrent {
		t.Fatalf("second install = %v, %v; want %v", outcome, err, commitscope.HookCurrent)
	}
	if report := commitscope.InstallReport(commitscope.HookCurrent, hooks); report != "" {
		t.Errorf("an unchanged install should say nothing, said:\n%s", report)
	}

	// An older copy of ours — the case that arises when the shim changes and
	// somebody's clone still holds last week's.
	if err := os.WriteFile(dest, []byte("#!/bin/sh\n# 🎯T377 v1\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if outcome, err := commitscope.InstallHook(hooks, shimPath(t)); err != nil || outcome != commitscope.HookUpdated {
		t.Fatalf("refresh = %v, %v; want %v", outcome, err, commitscope.HookUpdated)
	}
	if got := readFile(t, dest); got != shim {
		t.Errorf("stale copy was not refreshed:\n%s", got)
	}
}

// TestInstallRejectsAnUnmarkableSource guards the marker itself: a shim that
// lost its 🎯T377 line would install once and then be unrecognisable as ours
// forever after, so every later `make` would call it foreign and stop
// updating it.
func TestInstallRejectsAnUnmarkableSource(t *testing.T) {
	source := filepath.Join(t.TempDir(), "pre-commit")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := commitscope.InstallHook(t.TempDir(), source); err == nil {
		t.Error("an unmarked source installed silently")
	}
}

// TestShippedShimCarriesTheMarker keeps the file on disk honest, since every
// install decision above is made by looking for that string in it.
func TestShippedShimCarriesTheMarker(t *testing.T) {
	if !strings.Contains(readFile(t, shimPath(t)), commitscope.HookMarker) {
		t.Errorf("scripts/hooks/pre-commit does not contain %q", commitscope.HookMarker)
	}
}

// TestAFreshCloneIsGuardedByInstallAlone is the end of the argument. No hook
// is installed by hand anywhere in this test: it runs `commitscope --install`
// exactly as the Makefile does, and then the incident from
// TestBareCommitCannotSweepAnotherWorkersStagedHunks is replayed against the
// clone that install produced.
func TestAFreshCloneIsGuardedByInstallAlone(t *testing.T) {
	dir := t.TempDir()
	bin := guardBinary(t)
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = hermeticEnv(dir, bin)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := git("init", "-q", "-b", "master", "."); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, kv := range [][2]string{{"user.email", "t@example.invalid"}, {"user.name", "t"}} {
		if out, err := git("config", kv[0], kv[1]); err != nil {
			t.Fatalf("git config: %v\n%s", err, out)
		}
	}
	// The clone as git leaves it: a scripts/hooks/pre-commit in the tree and
	// nothing in .git/hooks.
	mustWrite(t, filepath.Join(dir, "scripts", "hooks", "pre-commit"), readFile(t, shimPath(t)), 0o755)
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("the fresh clone already has a pre-commit hook (%v)", err)
	}

	mustWrite(t, filepath.Join(dir, workerAPath), "base\n", 0o644)
	mustWrite(t, filepath.Join(dir, workerBPath), "base\n", 0o644)
	if out, err := git("add", "."); err != nil {
		t.Fatalf("seed add: %v\n%s", err, out)
	}
	if out, err := git("commit", "-m", "base"); err != nil {
		t.Fatalf("seed commit: %v\n%s", err, out)
	}

	// What `make` runs.
	install := exec.Command(bin, "--install")
	install.Dir = dir
	install.Env = hermeticEnv(dir, bin)
	out, err := install.CombinedOutput()
	if err != nil {
		t.Fatalf("commitscope --install: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "installed") {
		t.Errorf("install said nothing about installing:\n%s", out)
	}

	// And now the incident cannot happen in this clone either.
	mustWrite(t, filepath.Join(dir, workerAPath), "A's 🎯T370 work\n", 0o644)
	if out, err := git("add", workerAPath); err != nil {
		t.Fatalf("worker A staging: %v\n%s", err, out)
	}
	mustWrite(t, filepath.Join(dir, workerBPath), "B's 🎯T372 work\n", 0o644)
	if out, err := git("add", workerBPath); err != nil {
		t.Fatalf("worker B staging: %v\n%s", err, out)
	}
	out2, err := git("commit", "-m", "refactor(web): T372 one pending-owner-turn contract")
	if err == nil {
		t.Fatalf("a bare commit succeeded in a clone that was only ever set up by --install:\n%s", out2)
	}
	if !strings.Contains(out2, workerAPath) {
		t.Errorf("refusal does not name the foreign path that would have been swept:\n%s", out2)
	}
}

// TestInstallFollowsARedirectedHooksPath covers the clone whose core.hooksPath
// points elsewhere: installing into .git/hooks there would look successful
// while git went on running something else.
func TestInstallFollowsARedirectedHooksPath(t *testing.T) {
	dir := t.TempDir()
	bin := guardBinary(t)
	elsewhere := filepath.Join(dir, "scripts", "githooks")

	for _, args := range [][]string{
		{"init", "-q", "-b", "master", "."},
		{"config", "core.hooksPath", elsewhere},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = hermeticEnv(dir, bin)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mustWrite(t, filepath.Join(dir, "scripts", "hooks", "pre-commit"), readFile(t, shimPath(t)), 0o755)

	install := exec.Command(bin, "--install")
	install.Dir = dir
	install.Env = hermeticEnv(dir, bin)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("commitscope --install: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "pre-commit")); err != nil {
		t.Errorf("hook was not installed where git would look: %v", err)
	}
}

func hermeticEnv(home, bin string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"JEVONS_COMMITSCOPE_BIN=" + bin,
	}
}

func shimPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "scripts", "hooks", "pre-commit")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustWrite(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

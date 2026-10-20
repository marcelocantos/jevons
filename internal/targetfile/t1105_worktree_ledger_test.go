// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T1105: a linked git worktree keys as the repository it belongs to, so a
// worktree-isolated worker is visible to engagement in its clone's ledger.

func t1105Write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// t1105FakeWorktree lays out on disk exactly what `git worktree add` writes —
// a main clone with a .git dir, and a linked tree whose .git file points at
// <clone>/.git/worktrees/<name>, which names the common dir in `commondir` —
// without running git. Both trees hold their own copy of the ledger.
func t1105FakeWorktree(t *testing.T, root, repo, name string) (clone, wt string) {
	t.Helper()
	clone = filepath.Join(root, repo)
	wt = filepath.Join(root, ".jevons-worktrees-"+repo, name)
	admin := filepath.Join(clone, ".git", "worktrees", name)
	t1105Write(t, filepath.Join(clone, ".git", "HEAD"), "ref: refs/heads/master\n")
	t1105Write(t, filepath.Join(admin, "commondir"), "../..\n")
	t1105Write(t, filepath.Join(wt, ".git"), "gitdir: "+admin+"\n")
	t1105Write(t, filepath.Join(clone, "bullseye.yaml"), "targets: {}\n")
	t1105Write(t, filepath.Join(wt, "bullseye.yaml"), "targets: {}\n")
	return clone, wt
}

func t1105Real(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestT1105WorktreeSharesCloneLedgerKey(t *testing.T) {
	root := t1105Real(t, t.TempDir())
	clone, wt := t1105FakeWorktree(t, root, "proj", "jv-t9-scout")

	want := filepath.Join(clone, "bullseye.yaml")
	if got := LedgerKey(clone); got != want {
		t.Fatalf("clone key = %q, want %q (main clone key must not change)", got, want)
	}
	if got := LedgerKey(wt); got != want {
		t.Fatalf("worktree key = %q, want the clone's %q", got, want)
	}
	if got := LedgerKey(filepath.Join(wt, "internal", "x")); got != want {
		t.Fatalf("worktree subdir key = %q, want %q", got, want)
	}
	if !SameLedgerAsWorkdirs(clone, wt) {
		t.Fatal("clone and its worktree must share a ledger")
	}

	// A second repository's worktree stays distinct from the first repo.
	other, otherWT := t1105FakeWorktree(t, root, "other", "jv-t9-scout")
	if SameLedgerAsWorkdirs(clone, other) || SameLedgerAsWorkdirs(wt, otherWT) || SameLedgerAsWorkdirs(clone, otherWT) {
		t.Fatal("distinct repositories must not share a ledger")
	}
}

func TestT1105RepoLedgerKeyPure(t *testing.T) {
	resolveTo := func(root string, ok bool) mainRootResolver {
		return func(string) (string, bool) { return root, ok }
	}
	cases := []struct {
		name, ledger, top string
		resolve           mainRootResolver
		want              string
	}{
		{"linked worktree re-roots", "/w/wt/bullseye.yaml", "/w/wt", resolveTo("/w/clone", true), "/w/clone/bullseye.yaml"},
		{"nested ledger keeps its relative path", "/w/wt/sub/bullseye.yaml", "/w/wt", resolveTo("/w/clone", true), "/w/clone/sub/bullseye.yaml"},
		{"main tree keys as itself", "/w/clone/bullseye.yaml", "/w/clone", resolveTo("", false), "/w/clone/bullseye.yaml"},
		{"ledger above the tree is not re-rooted", "/w/bullseye.yaml", "/w/wt", resolveTo("/w/clone", true), "/w/bullseye.yaml"},
		{"no git tree", "/w/bullseye.yaml", "", resolveTo("/w/clone", true), "/w/bullseye.yaml"},
		{"no resolver", "/w/wt/bullseye.yaml", "/w/wt", nil, "/w/wt/bullseye.yaml"},
	}
	for _, c := range cases {
		if got := repoLedgerKey(c.ledger, c.top, c.resolve); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A submodule's .git file has no commondir behind it: it is its own repo.
func TestT1105SubmoduleIsNotAWorktree(t *testing.T) {
	root := t1105Real(t, t.TempDir())
	super := filepath.Join(root, "super")
	sub := filepath.Join(super, "vendor", "lib")
	modDir := filepath.Join(super, ".git", "modules", "lib")
	t1105Write(t, filepath.Join(modDir, "HEAD"), "ref: refs/heads/master\n")
	t1105Write(t, filepath.Join(sub, ".git"), "gitdir: ../../.git/modules/lib\n")
	t1105Write(t, filepath.Join(super, "bullseye.yaml"), "targets: {}\n")
	t1105Write(t, filepath.Join(sub, "bullseye.yaml"), "targets: {}\n")
	if got, want := LedgerKey(sub), filepath.Join(sub, "bullseye.yaml"); got != want {
		t.Fatalf("submodule key = %q, want %q", got, want)
	}
	if SameLedgerAsWorkdirs(super, sub) {
		t.Fatal("a submodule's ledger is not its superproject's")
	}
}

// One real `git worktree add`, so the file-reading resolver is held to what
// git itself writes, and to `git rev-parse --git-common-dir`.
//
// 🎯T440 //worktreereap:exempt — the worktree belongs to a throwaway repo
// inside t.TempDir, not to the shared clone: its admin entry lives in that
// repo's own .git and both vanish with the temp dir, so there is no shared
// `git worktree list` row for an owner mark to protect.
func TestT1105RealGitWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t1105Real(t, t.TempDir())
	clone := filepath.Join(root, "repo")
	wt := filepath.Join(root, ".jevons-worktrees-repo", "jv-t9-scout")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	git(clone, "init", "-q", "-b", "master")
	t1105Write(t, filepath.Join(clone, "bullseye.yaml"), "targets: {}\n")
	git(clone, "add", "bullseye.yaml")
	git(clone, "commit", "-q", "-m", "init")
	git(clone, "worktree", "add", "-q", "-b", "jevons-worktree/jv-t9-scout", wt, "HEAD")

	if _, err := os.Stat(filepath.Join(wt, "bullseye.yaml")); err != nil {
		t.Fatalf("worktree must carry its own ledger copy: %v", err)
	}
	common := filepath.Clean(strings.TrimSpace(git(wt, "rev-parse", "--path-format=absolute", "--git-common-dir")))
	if got, ok := gitMainRoot(wt); !ok || got != filepath.Dir(t1105Real(t, common)) {
		t.Fatalf("gitMainRoot(worktree) = %q,%v; git-common-dir %q", got, ok, common)
	}
	if _, ok := gitMainRoot(clone); ok {
		t.Fatal("the main clone is not a linked worktree")
	}
	want := filepath.Join(clone, "bullseye.yaml")
	if got := LedgerKey(wt); got != want {
		t.Fatalf("LedgerKey(worktree) = %q, want %q", got, want)
	}
	if !SameLedgerAsWorkdirs(clone, wt) {
		t.Fatal("real worktree must share its clone's ledger")
	}
}

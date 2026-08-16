// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildsnap_test

// 🎯T473 acceptance oracle: the snapshot build must resolve go.mod's local
// replace directives on a host nobody hand-patched.
//
// The load-bearing pair, over one scenario — a module whose replacement lives
// beside the CLONE and nowhere near the snapshot:
//
//	TestSnapshotResolvesLocalReplace — the snapshot build succeeds.
//	TestSnapshotReplaceIsUnresolvedWithoutRewrite — the control: the same
//	  worktree built without the rewrite FAILS, with the same "replacement
//	  directory … does not exist" the real incident produced. Without it, a
//	  fixture that stopped reproducing the fault would leave the first test
//	  green and empty.
//
// Nothing here reads or depends on the host's ~/.jevons/claudia symlink: the
// clone, the sibling and the snapshot are all fresh temp directories, so a
// machine that still has the hand-made symlink cannot hide a red.
//
// The fixture resolves the replacement the way go does — relative to the
// directory being built — rather than invoking a real toolchain, for the same
// reason the rest of this suite compiles a text file: the contract under test
// is what the snapshot's go.mod SAYS, not what a compiler does with it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The recipe stands in for the module loader: it reads the replacement path
// out of go.mod and resolves it against its own directory, failing with go's
// own wording when it is not there.
const replaceFixtureMakefile = `bin/artifact: src.txt go.mod
	@mkdir -p bin
	@dir=$$(awk '/^replace /{print $$4}' go.mod); \
	  test -d "$$dir" || { echo "example.com/sib@v0.0.0: replacement directory $$dir does not exist" >&2; exit 1; }
	@cp src.txt bin/artifact
`

const fixtureGoMod = "module example.com/root\n\ngo 1.26\n\nreplace example.com/sib => ../sib\n"

// newReplaceRepo makes a clone whose go.mod replaces a module with a sibling
// checkout, and puts that sibling where the clone — and only the clone — can
// see it. The snapshot goes under a different parent, exactly as
// ~/.jevons/build-snapshot does relative to ~/work/github.com/marcelocantos.
func newReplaceRepo(t *testing.T) (root, sib string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "root")
	sib = filepath.Join(base, "sib")

	write(t, filepath.Join(sib, "go.mod"), "module example.com/sib\n\ngo 1.26\n")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, root, "git", "init", "-q", "-b", "master")
	mustRun(t, root, "git", "config", "user.email", "t@example.com")
	mustRun(t, root, "git", "config", "user.name", "T")
	write(t, filepath.Join(root, "Makefile"), replaceFixtureMakefile)
	write(t, filepath.Join(root, "go.mod"), fixtureGoMod)
	write(t, filepath.Join(root, "src.txt"), goodSrc)
	write(t, filepath.Join(root, ".gitignore"), "bin/\n")
	mustRun(t, root, "git", "add", "Makefile", "go.mod", "src.txt", ".gitignore")
	mustRun(t, root, "git", "commit", "-q", "-m", "initial")
	return root, sib
}

// snapDir returns a snapshot path under a parent that does NOT contain the
// sibling — the whole point of the defect.
func snapDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "snap")
}

// TestSnapshotResolvesLocalReplace is the target's acceptance, stated as the
// incident: no symlink anywhere, and the snapshot build goes green.
func TestSnapshotResolvesLocalReplace(t *testing.T) {
	root, _ := newReplaceRepo(t)

	if err := build(t, root, snapDir(t)); err != nil {
		t.Fatalf("snapshot build could not resolve go.mod's local replace — "+
			"this is the 2026-08-15 outage, unfixed: %v", err)
	}
	if got := artifact(t, root); got != goodSrc {
		t.Errorf("artifact = %q, want HEAD's %q", got, goodSrc)
	}
}

// TestSnapshotReplaceIsUnresolvedWithoutRewrite is the control. It builds the
// same worktree the pre-fix code built — checked out, go.mod untouched — and
// asserts it fails the way the incident did.
func TestSnapshotReplaceIsUnresolvedWithoutRewrite(t *testing.T) {
	root, _ := newReplaceRepo(t)
	snap := snapDir(t)

	mustRun(t, root, "git", "worktree", "add", "--detach", "-q", snap, "HEAD")
	defer func() { _, _ = run(t, root, "git", "worktree", "remove", "--force", snap) }()

	out, err := run(t, snap, "make", "-C", snap, "bin/artifact")
	if err == nil {
		t.Fatalf("building the snapshot with go.mod's relative replace SUCCEEDED; "+
			"the scenario no longer reproduces the defect, so the test above proves nothing:\n%s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Errorf("the pre-fix build failed for some other reason than the unresolved replacement:\n%s", out)
	}
}

// TestSnapshotIsCleanAfterBuild keeps the rewrite from costing every later
// restart a cold build: prepareSnapshot reuses a worktree only while it is
// clean, so go.mod must be exactly as committed once the build is done.
func TestSnapshotIsCleanAfterBuild(t *testing.T) {
	root, _ := newReplaceRepo(t)
	snap := snapDir(t)

	if err := build(t, root, snap); err != nil {
		t.Fatalf("build: %v", err)
	}
	if out := mustRun(t, snap, "git", "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Errorf("the snapshot was left dirty, so every later restart rebuilds from cold:\n%s", out)
	}
	b, err := os.ReadFile(filepath.Join(snap, "go.mod"))
	if err != nil || string(b) != fixtureGoMod {
		t.Errorf("go.mod was not restored: %q (err %v)", string(b), err)
	}
}

// TestSnapshotReplaceMissingTargetIsNamed is the audible half (acceptance 3).
// When the replacement is not next to the clone either, the failure must name
// the directory it looked for and what to do — and must leave the installed
// binary alone, since a stale daemon serving under a fresh commit's name is
// the 🎯T194 failure this whole path exists to prevent.
func TestSnapshotReplaceMissingTargetIsNamed(t *testing.T) {
	root, sib := newReplaceRepo(t)
	if err := os.RemoveAll(sib); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "bin", "artifact"), "PREVIOUS BUILD\n")

	err := build(t, root, snapDir(t))
	if err == nil {
		t.Fatal("a build whose replacement does not exist reported success")
	}
	msg := err.Error()
	for _, want := range []string{"example.com/sib", sib, "🎯T473"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not name %q, so the operator cannot act on it:\n%s", want, msg)
		}
	}
	if got := artifact(t, root); got != "PREVIOUS BUILD\n" {
		t.Errorf("the failed build damaged the installed artifact: %q", got)
	}
}

// TestReplaceRewriteSkipsNonModules pins the narrowness: a tree with no go.mod
// is not touched at all, so the ordinary case cannot be broken by this path.
func TestReplaceRewriteSkipsNonModules(t *testing.T) {
	root := newRepo(t)
	if err := build(t, root, snapDir(t)); err != nil {
		t.Fatalf("a repo with no go.mod must still build: %v", err)
	}
	if got := artifact(t, root); got != goodSrc {
		t.Errorf("artifact = %q, want %q", got, goodSrc)
	}
}

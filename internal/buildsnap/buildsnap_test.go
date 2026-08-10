// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildsnap_test

// 🎯T254.2 acceptance oracle: one worker's uncommitted edits must not be able
// to stop another worker rebuilding and activating a landed change.
//
// The load-bearing pair is the mutation evidence, run as two tests over the
// identical scenario:
//
//	TestBrokenUncommittedFileDoesNotBlockRebuild — snapshot build: the shared
//	tree holds a file that does not compile, and the rebuild succeeds anyway.
//	TestSharedTreeBuildIsBlockedByForeignEdit — the pre-fix behaviour, building
//	the shared tree directly, asserted as a fact: it FAILS. If that ever stops
//	being true the first test proves nothing, and this fails loudly instead of
//	going quietly vacuous.
//
// Both directions are covered. Over-broadness is the second concern here, and
// the sharper one: a snapshot builder that shrugs off a genuinely broken HEAD
// would leave a stale binary serving while reporting success — precisely the
// failure 🎯T194 exists to prevent. TestBrokenHeadFailsLoudly pins that, and
// TestUncommittedGoodChangeIsNotBuilt pins the deliberate tradeoff so nobody
// can later "fix" it into building the working tree again.
//
// The fixture compiles a text file rather than Go source: the contract under
// test is which TREE is built, not what a compiler does with it, and a fake
// toolchain keeps the test hermetic and fast.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/buildsnap"
)

// The fixture Makefile "compiles" src.txt by copying it, and fails when the
// source contains the poison line — a stand-in for the real incident's
// uncommitted chat.go calling a method that did not exist.
const fixtureMakefile = `bin/artifact: src.txt
	@mkdir -p bin
	@grep -q BROKEN src.txt && { echo "compile error: undefined symbol" >&2; exit 1; } || true
	@cp src.txt bin/artifact
`

const goodSrc = "GOOD v1\n"
const brokenSrc = "BROKEN — this worker is mid-edit\n"

func run(t *testing.T, dir, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := run(t, dir, name, args...)
	if err != nil {
		t.Fatalf("%s %s in %s: %v\n%s", name, strings.Join(args, " "), dir, err, out)
	}
	return out
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The tests drive buildsnap.Run directly rather than a built cmd/buildsnap.
//
// That is a correctness requirement, not a style choice. An external test
// package that only exec's a binary has no compile-time dependency on the code
// it is checking, so `go test` caches its result and happily reports a green
// PASS for a package whose source has since changed — this suite did exactly
// that, reporting "ok (cached)" for a mutant it had never run. Importing the
// package makes the cache key include the product, which is the only way the
// standing gate can be trusted. cmd/buildsnap is a flag-parsing wrapper.

// newRepo makes a git repo whose HEAD builds cleanly.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustRun(t, root, "git", "init", "-q", "-b", "master")
	mustRun(t, root, "git", "config", "user.email", "t@example.com")
	mustRun(t, root, "git", "config", "user.name", "T")
	write(t, filepath.Join(root, "Makefile"), fixtureMakefile)
	write(t, filepath.Join(root, "src.txt"), goodSrc)
	write(t, filepath.Join(root, ".gitignore"), "bin/\n")
	mustRun(t, root, "git", "add", "Makefile", "src.txt", ".gitignore")
	mustRun(t, root, "git", "commit", "-q", "-m", "initial")
	return root
}

// build runs one snapshot build against root, the way restart-daily does.
func build(t *testing.T, root, snap string) error {
	t.Helper()
	_, err := buildsnap.Run(buildsnap.Config{
		RepoRoot: root,
		SnapDir:  snap,
		Target:   "bin/artifact",
		Artifact: "bin/artifact",
		Dest:     filepath.Join(root, "bin", "artifact"),
		Log:      func(s string) { t.Log(s) },
	})
	return err
}

// snapBuild is the common case: a fresh snapshot directory per call.
func snapBuild(t *testing.T, root string) error {
	t.Helper()
	return build(t, root, filepath.Join(t.TempDir(), "snap"))
}

func artifact(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "bin", "artifact"))
	if err != nil {
		return ""
	}
	return string(b)
}

// TestBrokenUncommittedFileDoesNotBlockRebuild is the target's acceptance,
// stated exactly as the incident: another worker's in-flight edit sits in the
// shared tree and does not compile. The rebuild must still succeed, and must
// produce HEAD's binary.
func TestBrokenUncommittedFileDoesNotBlockRebuild(t *testing.T) {
	root := newRepo(t)
	write(t, filepath.Join(root, "src.txt"), brokenSrc) // a foreign worker, mid-edit

	if err := snapBuild(t, root); err != nil {
		t.Fatalf("snapshot build failed on a foreign worker's broken edit — the incident is not fixed: %v", err)
	}
	if got := artifact(t, root); got != goodSrc {
		t.Errorf("artifact = %q, want HEAD's %q", got, goodSrc)
	}
	// The shared tree must be left exactly as the other worker had it.
	b, err := os.ReadFile(filepath.Join(root, "src.txt"))
	if err != nil || string(b) != brokenSrc {
		t.Errorf("the shared working tree was modified: %q (err %v)", string(b), err)
	}
}

// TestSharedTreeBuildIsBlockedByForeignEdit is the control. It reproduces the
// pre-fix path — build the shared tree directly — and asserts it fails. Without
// this, a fixture that quietly stopped reproducing the fault would leave the
// test above green and meaningless.
func TestSharedTreeBuildIsBlockedByForeignEdit(t *testing.T) {
	root := newRepo(t)
	write(t, filepath.Join(root, "src.txt"), brokenSrc)

	out, err := run(t, root, "make", "-C", root, "bin/artifact")
	if err == nil {
		t.Fatalf("building the shared tree SUCCEEDED with a broken uncommitted file; "+
			"the scenario no longer reproduces the defect, so the snapshot test proves nothing:\n%s", out)
	}
}

// TestBrokenHeadFailsLoudly is the over-broadness guard, and the one that
// matters most. A snapshot builder must not turn "HEAD is broken" into a quiet
// success that leaves a stale binary serving.
func TestBrokenHeadFailsLoudly(t *testing.T) {
	root := newRepo(t)
	// A previously installed artifact, as a running daemon would have.
	write(t, filepath.Join(root, "bin", "artifact"), "PREVIOUS BUILD\n")

	write(t, filepath.Join(root, "src.txt"), brokenSrc)
	mustRun(t, root, "git", "commit", "-q", "-a", "-m", "break HEAD")

	if err := snapBuild(t, root); err == nil {
		t.Fatal("a broken HEAD built successfully; a stale binary would be activated as if fresh")
	}
	if got := artifact(t, root); got != "PREVIOUS BUILD\n" {
		t.Errorf("failed build damaged the installed artifact: %q", got)
	}
}

// TestPartialBuildIsNotInstalled is the sharp end of the over-broadness
// direction. A make run can write its output and THEN fail — a later recipe
// line, a subsequent target, a link step after codegen. If the build's exit
// status is not checked, an artifact exists on disk and gets installed, and the
// daemon is activated on a half-built binary while every log line says success.
//
// This test exists because it is the ONLY one that distinguishes checking the
// status from not checking it: when a failed build leaves no output at all, the
// install step fails anyway and a mutant that ignores the build error passes
// every other test here. It did exactly that before this case was added.
func TestPartialBuildIsNotInstalled(t *testing.T) {
	root := newRepo(t)
	write(t, filepath.Join(root, "bin", "artifact"), "PREVIOUS BUILD\n")
	write(t, filepath.Join(root, "Makefile"), "bin/artifact: src.txt\n"+
		"\t@mkdir -p bin\n"+
		"\t@printf 'HALF-BUILT\\n' > bin/artifact\n"+
		"\t@echo 'link failed' >&2; exit 1\n")
	mustRun(t, root, "git", "commit", "-q", "-a", "-m", "build fails after writing output")

	if err := snapBuild(t, root); err == nil {
		t.Fatal("a build that failed after writing its output reported success")
	}
	if got := artifact(t, root); got != "PREVIOUS BUILD\n" {
		t.Errorf("a half-built artifact was installed: %q — the daemon would run it as if fresh", got)
	}
}

// TestUncommittedGoodChangeIsNotBuilt pins the deliberate tradeoff. Building
// HEAD is what makes one worker's edits unable to break another's activation;
// if someone later "fixes" this into building the working tree, the incident
// comes straight back and this test says so.
func TestUncommittedGoodChangeIsNotBuilt(t *testing.T) {
	root := newRepo(t)
	write(t, filepath.Join(root, "src.txt"), "GOOD v2 uncommitted\n")

	if err := snapBuild(t, root); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if got := artifact(t, root); got != goodSrc {
		t.Errorf("artifact = %q; the working tree was built instead of HEAD", got)
	}
}

// TestSnapshotFollowsHead keeps the reused worktree from going stale: a second
// run after a new commit must build the new commit, not the cached checkout.
func TestSnapshotFollowsHead(t *testing.T) {
	root := newRepo(t)
	snap := filepath.Join(t.TempDir(), "snap")

	if err := build(t, root, snap); err != nil {
		t.Fatalf("first build: %v", err)
	}
	if got := artifact(t, root); got != goodSrc {
		t.Fatalf("first artifact = %q", got)
	}

	write(t, filepath.Join(root, "src.txt"), "GOOD v2\n")
	mustRun(t, root, "git", "commit", "-q", "-a", "-m", "v2")

	if err := build(t, root, snap); err != nil {
		t.Fatalf("second build: %v", err)
	}
	if got := artifact(t, root); got != "GOOD v2\n" {
		t.Errorf("second artifact = %q, want the new HEAD %q — the snapshot went stale", got, "GOOD v2\n")
	}
}

// TestDirtySnapshotIsRecovered covers the operational case where something
// wrote into the snapshot: it must be discarded and rebuilt, never compiled as
// found, and never repaired with `git reset --hard`.
func TestDirtySnapshotIsRecovered(t *testing.T) {
	root := newRepo(t)
	snap := filepath.Join(t.TempDir(), "snap")
	if err := build(t, root, snap); err != nil {
		t.Fatalf("first build: %v", err)
	}

	// Someone scribbles in the snapshot.
	write(t, filepath.Join(snap, "src.txt"), brokenSrc)

	if err := build(t, root, snap); err != nil {
		t.Fatalf("dirty snapshot was not recovered: %v", err)
	}
	if got := artifact(t, root); got != goodSrc {
		t.Errorf("artifact = %q; the dirty snapshot was built as found", got)
	}
}

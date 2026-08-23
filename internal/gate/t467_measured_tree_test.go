// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// 🎯T467: provenance describes the tree the command ran in, not the launcher.

// twoTrees builds a dirty shared clone and a clean detached worktree of the
// same HEAD. The dirty tree carries an uncommitted neighbour file; the clean
// worktree does not.
func twoTrees(t *testing.T) (dirty, clean, head string) {
	t.Helper()
	dirty, head = fixtureRepo(t, true)
	writeFixture(t, dirty, "neighbour-wip.txt", "uncommitted neighbour work\n")

	scratch := t.TempDir()
	clean = filepath.Join(scratch, "clean")
	fixtureGit(t, dirty, "worktree", "add", "--detach", clean, "HEAD")
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: clean, Note: t.Name()}); err != nil {
		t.Fatalf("mark worktree: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runGit(dirty, "worktree", "remove", "--force", clean)
		_, _ = runGit(dirty, "worktree", "prune")
	})

	if p := ProbeTree(dirty); p == nil || p.Clean || p.DirtyFiles == 0 {
		t.Fatalf("fixture dirty tree is not dirty: %+v", p)
	}
	if p := ProbeTree(clean); p == nil || !p.Clean {
		t.Fatalf("fixture clean worktree is not clean: %+v", p)
	}
	return dirty, clean, head
}

// TestT467GateFromDirtyCloneMeasuresCleanWorktreeViaDashC is the live
// incident in miniature: launch from a dirty shared clone, ask the command to
// run in a clean worktree with -C, and the attestation must name the clean
// tree — not tree=dirty+N of the launcher.
func TestT467GateFromDirtyCloneMeasuresCleanWorktreeViaDashC(t *testing.T) {
	dirty, clean, head := twoTrees(t)
	store := storeAt(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getcwd: %v", err)
	}
	if err := os.Chdir(dirty); err != nil {
		t.Fatalf("chdir dirty: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	rec, err := Run(&RunArgs{
		Command: []string{"git", "-C", clean, "rev-parse", "HEAD"},
		// Dir deliberately empty: the gate was launched from dirty, and the
		// command's -C is the only signal naming the measured tree.
		Store:  store,
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Tree == nil {
		t.Fatal("provenance unknown; want the clean worktree")
	}
	if !rec.Tree.Clean {
		t.Fatalf("launched from dirty but measured dirty tree: %+v\nattestation: %s", rec.Tree, rec.Attestation())
	}
	if rec.Tree.Commit != head {
		t.Errorf("measured commit %q, want %q", rec.Tree.Commit, head)
	}
	if !samePath(rec.Tree.Repo, clean) {
		t.Errorf("measured repo %q, want clean worktree %q", rec.Tree.Repo, clean)
	}
	if !strings.Contains(rec.Attestation(), "tree=clean@") {
		t.Errorf("attestation does not carry clean token: %s", rec.Attestation())
	}
	if strings.Contains(rec.Attestation(), "tree=dirty+") {
		t.Errorf("attestation still names the launcher's dirty tree: %s", rec.Attestation())
	}
}

// TestT467GateFromCleanCheckoutMeasuresDirtyTreeViaDashC is the dangerous
// inverse: launch from clean, point the command at a dirty tree, must not
// print tree=clean@.
func TestT467GateFromCleanCheckoutMeasuresDirtyTreeViaDashC(t *testing.T) {
	dirty, clean, head := twoTrees(t)
	store := storeAt(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getcwd: %v", err)
	}
	if err := os.Chdir(clean); err != nil {
		t.Fatalf("chdir clean: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	rec, err := Run(&RunArgs{
		Command: []string{"git", "-C", dirty, "status", "--porcelain"},
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Tree == nil {
		t.Fatal("provenance unknown; want the dirty tree")
	}
	if rec.Tree.Clean || rec.Tree.DirtyFiles == 0 {
		t.Fatalf("launched from clean but measured clean: %+v\nattestation: %s", rec.Tree, rec.Attestation())
	}
	if rec.Tree.Commit != head {
		t.Errorf("measured commit %q, want %q", rec.Tree.Commit, head)
	}
	if !samePath(rec.Tree.Repo, dirty) {
		t.Errorf("measured repo %q, want dirty tree %q", rec.Tree.Repo, dirty)
	}
	if !strings.Contains(rec.Attestation(), "tree=dirty+") {
		t.Errorf("attestation does not carry dirty token: %s", rec.Attestation())
	}
	if strings.Contains(rec.Attestation(), "tree=clean@") {
		t.Errorf("attestation laundered a dirty measurement as clean: %s", rec.Attestation())
	}
}

// TestT467OpaqueShellProvenanceIsUnknown pins fail-closed: an undeterminable
// command must not inherit the launcher's tree state.
func TestT467OpaqueShellProvenanceIsUnknown(t *testing.T) {
	dirty, _, _ := twoTrees(t)
	store := storeAt(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getcwd: %v", err)
	}
	if err := os.Chdir(dirty); err != nil {
		t.Fatalf("chdir dirty: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	rec, err := Run(&RunArgs{
		Command: []string{"sh", "-c", "exit 0"},
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Tree != nil {
		t.Fatalf("opaque shell stamped launcher tree %+v; want unknown", rec.Tree)
	}
	if strings.Contains(rec.Attestation(), "tree=") {
		t.Errorf("attestation claims a tree for an undeterminable command: %s", rec.Attestation())
	}
}

// TestT467MalformedDashCIsUnknown: a declared but unusable chdir is not the
// launcher, and not inventable.
func TestT467MalformedDashCIsUnknown(t *testing.T) {
	dirty, _, _ := twoTrees(t)
	store := storeAt(t)

	rec, err := Run(&RunArgs{
		Command: []string{"git", "-C"},
		Dir:     dirty,
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	// Command fails to start or exits non-zero; provenance is decided first.
	if rec == nil {
		t.Fatalf("expected a record even when the command is malformed (err=%v)", err)
	}
	if rec.Tree != nil {
		t.Fatalf("malformed -C stamped %+v; want unknown (not Dir=%s)", rec.Tree, dirty)
	}
}

// TestT467ExplicitGateDirStillWinsWithoutCommandChdir keeps -dir / RunClean
// behaviour: when the command does not redirect, the gate Dir is the tree.
func TestT467ExplicitGateDirStillWinsWithoutCommandChdir(t *testing.T) {
	dirty, clean, head := twoTrees(t)
	store := storeAt(t)

	rec, err := Run(&RunArgs{
		Command: []string{"git", "rev-parse", "HEAD"},
		Dir:     clean,
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Tree == nil || !rec.Tree.Clean {
		t.Fatalf("explicit Dir=clean recorded %+v", rec.Tree)
	}
	if rec.Tree.Commit != head {
		t.Errorf("commit %q, want %q", rec.Tree.Commit, head)
	}

	// And command -C still overrides an explicit Dir (the inverse shape).
	rec, err = Run(&RunArgs{
		Command: []string{"git", "-C", dirty, "rev-parse", "HEAD"},
		Dir:     clean,
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run with -C override: %v", err)
	}
	if rec.Tree == nil || rec.Tree.Clean {
		t.Fatalf("-C dirty with Dir=clean recorded %+v; want dirty", rec.Tree)
	}
}

// samePath compares filesystem paths after EvalSymlinks so macOS /var vs
// /private/var does not fail an otherwise correct provenance check.
func samePath(a, b string) bool {
	ea, errA := filepath.EvalSymlinks(a)
	eb, errB := filepath.EvalSymlinks(b)
	if errA != nil {
		ea = filepath.Clean(a)
	}
	if errB != nil {
		eb = filepath.Clean(b)
	}
	return ea == eb
}

func TestResolveMeasuredDirTable(t *testing.T) {
	tmp := t.TempDir()
	absClean := filepath.Join(tmp, "clean")
	if err := os.Mkdir(absClean, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		argv    []string
		gateDir string
		wantDir string
		wantOK  bool
	}{
		{"go -C abs", []string{"go", "test", "-C", absClean, "./pkg"}, "", absClean, true},
		{"go leading -C", []string{"go", "-C", absClean, "test", "./pkg"}, "", absClean, true},
		{"git -C abs", []string{"git", "-C", absClean, "status"}, "/launcher", absClean, true},
		{"make -C abs", []string{"make", "-C", absClean, "test"}, "", absClean, true},
		{"make --directory=", []string{"make", "--directory=" + absClean, "test"}, "", absClean, true},
		{"make glued -C", []string{"make", "-C" + absClean, "test"}, "", absClean, true},
		{"relative -C against gateDir", []string{"go", "test", "-C", "clean", "./x"}, tmp, absClean, true},
		{"explicit dir, no -C", []string{"go", "test", "./x"}, absClean, absClean, true},
		{"plain go, empty dir", []string{"go", "test", "./x"}, "", "", true},
		{"opaque sh -c no gateDir", []string{"sh", "-c", "true"}, "", "", false},
		{"opaque bash -c with gateDir", []string{"bash", "-c", "true"}, absClean, absClean, true},
		{"malformed -C", []string{"git", "-C"}, "", "", false},
		{"malformed -C ignores gateDir", []string{"git", "-C"}, absClean, "", false},
		{"grep -C is not a chdir", []string{"grep", "-C", "3", "pat", "file"}, absClean, absClean, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolveMeasuredDir(tc.argv, tc.gateDir)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v (dir=%q)", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if filepath.Clean(got) != filepath.Clean(tc.wantDir) {
				t.Fatalf("dir=%q, want %q", got, tc.wantDir)
			}
		})
	}
}

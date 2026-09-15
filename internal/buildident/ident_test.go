// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildident_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/buildident"
)

// workspace builds a throwaway ../go.work layout: a jevons repo and a
// claudia sibling, each its own git checkout, with the workspace file
// beside them exactly as the real tree has it.
type workspace struct {
	t    *testing.T
	base string
	repo string
	sib  string
}

func newWorkspace(t *testing.T) *workspace {
	t.Helper()
	base := t.TempDir()
	w := &workspace{t: t, base: base,
		repo: filepath.Join(base, "jevons"),
		sib:  filepath.Join(base, "claudia"),
	}
	for _, dir := range []string{w.repo, w.sib} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		w.git(dir, "init")
		w.git(dir, "config", "user.email", "t580@test")
		w.git(dir, "config", "user.name", "t580")
		w.write(dir, "a.txt", "base\n")
		w.git(dir, "add", "a.txt")
		w.git(dir, "commit", "-m", "base")
	}
	w.write(base, "go.work", "go 1.26.1\n\nuse (\n\t./claudia\n\t./jevons\n)\n")
	return w
}

func (w *workspace) git(dir string, args ...string) {
	w.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		w.t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func (w *workspace) write(dir, name, body string) {
	w.t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) identity() buildident.Report {
	w.t.Helper()
	r, err := buildident.Compute(w.repo)
	if err != nil {
		w.t.Fatalf("Compute: %v", err)
	}
	if r.Identity == "" {
		w.t.Fatal("empty identity")
	}
	return r
}

// TestSiblingHEADChangesIdentity is 🎯T580's oracle: two runs differing only
// in the claudia sibling's HEAD must produce different identities. Before
// this, the restart hashed bin/jevonsd alone and reported "already
// activated" for a build whose only change came from the sibling.
func TestSiblingHEADChangesIdentity(t *testing.T) {
	w := newWorkspace(t)
	before := w.identity()

	w.write(w.sib, "fix.txt", "readiness fix\n")
	w.git(w.sib, "add", "fix.txt")
	w.git(w.sib, "commit", "-m", "sibling-only fix")

	after := w.identity()
	if after.Identity == before.Identity {
		t.Fatalf("sibling HEAD moved and the identity did not:\n%s", buildident.FormatHuman(after))
	}
	if before.Repo.HEAD != after.Repo.HEAD {
		t.Fatalf("the repo HEAD was supposed to be the constant: %s vs %s", before.Repo.HEAD, after.Repo.HEAD)
	}
	// Recomputing with nothing changed must be stable, or every restart
	// would bounce and the check would be worthless in the other direction.
	if again := w.identity(); again.Identity != after.Identity {
		t.Fatalf("identity is not stable across runs: %s vs %s", after.Identity, again.Identity)
	}
}

// A sibling edit that is not yet committed changes the build too: the
// development builds consume local-master claudia through go.work (🎯T448), so
// the working tree is what gets compiled.
func TestSiblingDirtyStateChangesIdentity(t *testing.T) {
	w := newWorkspace(t)
	before := w.identity()

	w.write(w.sib, "a.txt", "edited in place\n")
	after := w.identity()
	if after.Identity == before.Identity {
		t.Fatal("uncommitted sibling edit did not move the identity")
	}
	if after.Siblings[0].Dirty == "" {
		t.Fatalf("sibling not reported dirty:\n%s", buildident.FormatHuman(after))
	}

	// A second edit to the same file must differ again — a porcelain status
	// line repeats, so status alone would collapse these two states.
	w.write(w.sib, "a.txt", "edited differently\n")
	if third := w.identity(); third.Identity == after.Identity {
		t.Fatal("a further edit to the same sibling file did not move the identity")
	}
}

// Without a workspace there is nothing to widen the identity with. That is
// allowed — a pristine clone and a buildsnap worktree both look like this —
// but it must be said in words, never silently narrower.
func TestNoWorkspaceDegradesAndSaysSo(t *testing.T) {
	w := newWorkspace(t)
	if err := os.Remove(filepath.Join(w.base, "go.work")); err != nil {
		t.Fatal(err)
	}
	r := w.identity()
	if len(r.Siblings) != 0 {
		t.Fatalf("siblings without a go.work: %+v", r.Siblings)
	}
	if !strings.Contains(r.Degraded, "no go.work") {
		t.Fatalf("degradation not named: %q", r.Degraded)
	}
	if !strings.Contains(buildident.FormatHuman(r), "DEGRADED") {
		t.Fatalf("human report hides the degradation:\n%s", buildident.FormatHuman(r))
	}
}

// A directory that is not a git work tree at all is an error, not an
// identity: an identity blind to this repo's own HEAD is not one.
func TestNonRepoRootIsAnError(t *testing.T) {
	if _, err := buildident.Compute(t.TempDir()); err == nil {
		t.Fatal("expected an error for a non-git root")
	}
}

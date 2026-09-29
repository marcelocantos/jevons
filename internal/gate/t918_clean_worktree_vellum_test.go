// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T440 //worktreereap:exempt — the worktree created here lives inside
// t.TempDir: no shared clone ever lists it, so there is nothing for the
// 🎯T440 sweeper to reap even if this test dies before cleanup.
// 🎯T918: scripts/journey-suite needs BOTH unpublished siblings — claudia
// (claudia/omp) and vellum. Gate f4c94df4, run from a fleet worktree, failed
// setup with "no required module provides package
// github.com/marcelocantos/claudia/omp" because the injector looked for
// siblings beside .jevons-worktrees-jevons. 🎯T909's test pins claudia alone;
// this one pins every listed sibling from a worktree one level off the org dir.
func TestInjectCleanSiblingGoWorkFromLinkedWorktreeCoversEverySibling(t *testing.T) {
	org, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(org, "jevons")
	writeGoMod(t, clone, "github.com/marcelocantos/jevons")
	gitIn(t, clone, "init", "-q", "-b", "master")
	gitIn(t, clone, "add", "go.mod")
	gitIn(t, clone, "commit", "-q", "-m", "init")
	for _, mod := range cleanSiblingModules {
		writeGoMod(t, filepath.Join(org, mod), "github.com/marcelocantos/"+mod)
	}

	root := filepath.Join(org, ".jevons-worktrees-jevons", "jv-worker")
	gitIn(t, clone, "worktree", "add", "-q", "--detach", root)

	wt := filepath.Join(org, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	restore := injectCleanSiblingGoWork(root, wt)
	defer restore()

	data, err := os.ReadFile(filepath.Join(wt, "go.work"))
	if err != nil {
		t.Fatalf("go.work not written for a linked worktree root: %v", err)
	}
	for _, mod := range []string{"claudia", "vellum"} {
		want := "replace github.com/marcelocantos/" + mod + " => " + filepath.Join(org, mod)
		if !strings.Contains(string(data), want) {
			t.Errorf("go.work does not point %s at the shared clone's sibling; want %q in:\n%s", mod, want, data)
		}
	}
}

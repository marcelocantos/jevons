// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T254.2: the sharing condition that sends a new worker into its own tree.
// A work agent anywhere in the repo counts — in the shared clone or in an
// isolated tree made from it — whether or not its process is up right now;
// the worker itself, other repos, and non-work seats do not.
func TestRepoSharedWithWorker(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "jevons")
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	register := func(name, workdir, purpose string) {
		t.Helper()
		if err := reg.Register(claudia.AgentDef{
			Name: name, WorkDir: workdir, SessionID: name + "-s", Purpose: purpose, Provider: "grok",
		}); err != nil {
			t.Fatal(err)
		}
	}

	register("jevons-po", base, "product-owner")
	register("other-repo-worker", filepath.Join(dir, "claudia"), claudia.PurposeWork)
	if s.repoSharedWithWorker(base, "jv-new") {
		t.Fatal("a PO and a worker in another repo made the clone count as shared")
	}

	register("jv-isolated", worktree.WorktreePath(base, "jv-isolated"), claudia.PurposeWork)
	if !s.repoSharedWithWorker(base, "jv-new") {
		t.Fatal("a worker in an isolated tree of the repo did not count: the next worker would get the shared clone")
	}
	if s.repoSharedWithWorker(base, "jv-isolated") {
		t.Fatal("a worker counted itself as sharing its repo")
	}

	register("jv-in-clone", base, "") // empty purpose is a work agent
	if !s.repoSharedWithWorker(base, "jv-isolated") {
		t.Fatal("a stopped-or-running work agent in the shared clone did not count")
	}
}

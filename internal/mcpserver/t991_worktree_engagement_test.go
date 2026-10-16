// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/targetfile"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// t991Repo builds a real git shared clone with a committed bullseye.yaml so
// worktree.Ensure produces a linked worktree that carries its own ledger copy.
func t991Repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "jevons")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = base
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("git", "init", "-b", "master")
	run("git", "config", "user.email", "t991@test")
	run("git", "config", "user.name", "t991")
	ledger := "schema_version: 1\ntargets:\n  Tn:\n    name: engagement\n    status: identified\n"
	if err := os.WriteFile(filepath.Join(base, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "README"), []byte("t991\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	return base
}

// 🎯T991 hermetic: a seat whose workdir is a linked worktree of a repo with a
// committed bullseye.yaml, target_id=Tn, counts as engaged when the T222
// second-implementer gate is scoped to the shared clone.
func TestT991WorktreeSeatEngagesSharedCloneTarget(t *testing.T) {
	base := t991Repo(t)
	wt, err := worktree.Ensure(base, "jv-t991-worker")
	if err != nil {
		t.Fatal(err)
	}
	// Precondition of the bug: worktree has its own bullseye.yaml path that
	// used to be a distinct LedgerKey.
	if targetfile.LedgerKey(base) == targetfile.LedgerKey(wt) {
		// Good — production fix is in place. Continue with engagement assert.
	} else {
		t.Fatalf("precondition failed: LedgerKey(shared)=%q LedgerKey(wt)=%q still differ",
			targetfile.LedgerKey(base), targetfile.LedgerKey(wt))
	}

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t991-worker", WorkDir: wt, SessionID: "s-wt",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok", TargetID: "Tn",
	}); err != nil {
		t.Fatal(err)
	}

	// Shared-clone scope must see the worktree seat.
	got := workAgentsEngagedOnTarget(reg, "Tn", base, "")
	if len(got) != 1 || got[0] != "jv-t991-worker" {
		t.Fatalf("shared-clone scope engaged=%v want [jv-t991-worker]", got)
	}
	// Worktree scope must see itself the same way.
	got = workAgentsEngagedOnTarget(reg, "Tn", wt, "")
	if len(got) != 1 || got[0] != "jv-t991-worker" {
		t.Fatalf("worktree scope engaged=%v want [jv-t991-worker]", got)
	}

	s := New(base, nil, nil)
	s.SetRegistry(reg)
	msg := s.refuseEngagedOrClosedTarget("jv-t991-second", base, "Tn", false)
	if msg == "" || !strings.Contains(msg, "jv-t991-worker") {
		t.Fatalf("T222 gate scoped to shared clone must refuse second spawn, got %q", msg)
	}
	// force_engage still overrides.
	if msg := s.refuseEngagedOrClosedTarget("jv-t991-second", base, "Tn", true); msg != "" {
		t.Fatalf("force_engage must allow: %q", msg)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T991: engagement scoped from the shared clone must see a worktree-isolated
// implementer and refuse a second spawn (🎯T222).

func t991FakeLinkedWorktree(t *testing.T, root string) (clone, wt, worker string) {
	t.Helper()
	clone = filepath.Join(root, "proj")
	worker = "jv-t501-scout"
	wt = filepath.Join(root, ".jevons-worktrees-proj", worker)
	admin := filepath.Join(clone, ".git", "worktrees", worker)
	t1105WriteFile(t, filepath.Join(clone, ".git", "HEAD"), "ref: refs/heads/master\n")
	t1105WriteFile(t, filepath.Join(admin, "commondir"), "../..\n")
	t1105WriteFile(t, filepath.Join(wt, ".git"), "gitdir: "+admin+"\n")
	t1105WriteFile(t, filepath.Join(clone, "bullseye.yaml"), frontierConsumeTestLedger)
	t1105WriteFile(t, filepath.Join(wt, "bullseye.yaml"), frontierConsumeTestLedger)
	return clone, wt, worker
}

func TestT991WorktreeSecondImplementerGateRefusedFromCloneScope(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clone, wt, worker := t991FakeLinkedWorktree(t, root)

	reg, err := claudia.NewRegistry(filepath.Join(root, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: worker, WorkDir: wt, SessionID: "s1",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T501",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}

	s := New(clone, nil, nil)
	s.SetRegistry(reg)

	msg := s.refuseEngagedOrClosedTarget("jv-t501-dup", clone, "T501", false)
	if msg == "" || !strings.Contains(msg, worker) {
		t.Fatalf("T222 gate from clone scope: want refuse naming %q, got %q", worker, msg)
	}
	if msg := s.refuseEngagedOrClosedTarget(worker, clone, "T501", false); msg != "" {
		t.Fatalf("same-name resume from clone scope must allow: %q", msg)
	}
}

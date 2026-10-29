// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1166 hermetic: agent_start engagement gate reads canonical ledger status
// after achieve→revert, not a stale per-worktree bullseye.yaml.
func TestT1166AgentStartAllowsKickoffAfterRevertOnCanonicalLedger(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const worker = "jv-t1166-worker"
	clone := filepath.Join(root, "jevons")
	wt := filepath.Join(root, ".jevons-worktrees-jevons", worker)
	admin := filepath.Join(clone, ".git", "worktrees", worker)
	t1105WriteFile(t, filepath.Join(clone, ".git", "HEAD"), "ref: refs/heads/master\n")
	t1105WriteFile(t, filepath.Join(admin, "commondir"), "../..\n")
	t1105WriteFile(t, filepath.Join(wt, ".git"), "gitdir: "+admin+"\n")
	const id = "T1166"
	t1105WriteFile(t, filepath.Join(clone, "bullseye.yaml"), `targets:
  `+id+`:
    name: reverted leaf
    status: converging
`)
	t1105WriteFile(t, filepath.Join(wt, "bullseye.yaml"), `targets:
  `+id+`:
    name: stale achieved in worktree
    status: achieved
`)

	reg, err := claudia.NewRegistry(filepath.Join(root, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(clone, nil, nil)
	s.SetRegistry(reg)

	// Same shape as handleAgentStart after worktree isolation: gate uses wt.
	if msg := s.refuseEngagedOrClosedTarget(worker, wt, id, false); msg != "" {
		t.Fatalf("kickoff must not refuse reverted target: %q", msg)
	}
}

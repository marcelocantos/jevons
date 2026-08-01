// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// Hermetic regression for 🎯T86: two EnsureAgent calls with the same
// workdir and different names must yield two defs, two SessionIDs, and
// leave the original name registered (no workdir-based session steal).
func TestEnsureAgentSameWorkdirDifferentNames(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const workDir = "/work/shared-repo"
	first, err := reg.EnsureAgent("worker-a", workDir, "", false)
	if err != nil {
		t.Fatalf("EnsureAgent worker-a: %v", err)
	}
	second, err := reg.EnsureAgent("worker-b", workDir, "", false)
	if err != nil {
		t.Fatalf("EnsureAgent worker-b: %v", err)
	}
	if first.Name != "worker-a" || second.Name != "worker-b" {
		t.Fatalf("names = %q, %q", first.Name, second.Name)
	}
	if first.SessionID == "" || second.SessionID == "" {
		t.Fatal("empty SessionID")
	}
	if first.SessionID == second.SessionID {
		t.Fatalf("SessionIDs not independent: both %q", first.SessionID)
	}
	if reg.Def("worker-a") == nil {
		t.Fatal("worker-a missing after second start")
	}
	if reg.Def("worker-a").SessionID != first.SessionID {
		t.Fatal("worker-a SessionID changed after worker-b EnsureAgent")
	}
	if got := reg.Def("worker-a").Name; got != "worker-a" {
		t.Fatalf("worker-a silently renamed to %q", got)
	}
	if len(reg.List()) != 2 {
		t.Fatalf("List len = %d, want 2", len(reg.List()))
	}
	// Display fragments for agent_list must also differ (UUID-v7 peers).
	if sessionDisplay(first.SessionID) == sessionDisplay(second.SessionID) {
		t.Fatalf("sessionDisplay collided: %q", sessionDisplay(first.SessionID))
	}
}

func TestEnsureAgentSameNameIdempotent(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := reg.EnsureAgent("overseer", "/work/repo", "grok", true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := reg.EnsureAgent("overseer", "/work/repo", "grok", true)
	if err != nil {
		t.Fatal(err)
	}
	if a.SessionID != b.SessionID {
		t.Fatalf("same-name EnsureAgent re-minted session: %q → %q", a.SessionID, b.SessionID)
	}
	if len(reg.List()) != 1 {
		t.Fatalf("List len = %d, want 1", len(reg.List()))
	}
}

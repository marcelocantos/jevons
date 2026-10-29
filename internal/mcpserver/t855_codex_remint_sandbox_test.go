// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// TestT855CodexRemintRestoresWorkspaceWrite is the acceptance repro: a codex
// work seat whose sandbox metadata was cleared by a cross-provider start must
// get workspace-write back on remint or switch onto codex, not claudia's empty
// SandboxMode → read-only default.
func TestT855CodexRemintRestoresWorkspaceWrite(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	workdir := t.TempDir()
	const name = "jv-t855-remint"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workdir, SessionID: "t855-codex",
		Provider: claudia.ProviderCodex, Purpose: claudia.PurposeWork,
		SandboxMode: "workspace-write", SandboxWritableRoots: []string{workdir},
		SandboxNetworkAccess: true, SandboxGitWrite: true,
	}); err != nil {
		t.Fatal(err)
	}
	reg.Stop(name)

	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	// codex → claude strips sandbox (🎯T1032 / 🎯T763).
	def, existed, _, err := s.stitchAgentStart(
		name, workdir, "", "claude", "", "jevons-po", claudia.PurposeWork, "T855", "",
	)
	if err != nil {
		t.Fatalf("stitchAgentStart codex→claude: %v", err)
	}
	if !existed || def.SandboxMode != "" {
		t.Fatalf("after codex→claude: existed=%v sandbox=%q", existed, def.SandboxMode)
	}

	// claude → codex remint must re-derive workspace-write (🎯T855).
	def, existed, _, err = s.stitchAgentStart(
		name, workdir, "", "codex", "", "jevons-po", claudia.PurposeWork, "T855", "",
	)
	if err != nil {
		t.Fatalf("stitchAgentStart claude→codex: %v", err)
	}
	if !existed {
		t.Fatal("expected resume of existing row")
	}
	if def.SandboxMode != "workspace-write" {
		t.Fatalf("SandboxMode = %q, want workspace-write after codex remint", def.SandboxMode)
	}
	if !def.SandboxGitWrite {
		t.Fatal("SandboxGitWrite = false after codex remint")
	}
	if len(def.SandboxWritableRoots) == 0 || !def.SandboxNetworkAccess {
		t.Fatalf("missing codex tuning after remint: roots=%v network=%v", def.SandboxWritableRoots, def.SandboxNetworkAccess)
	}
}

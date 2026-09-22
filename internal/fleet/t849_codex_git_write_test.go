// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// The git grant rides the same decision as the mode, so no mint path can
// set one without the other (🎯T849, the shape of 🎯T598's
// TestTuningTracksTheSandboxMode).
//
// The failure this pins is not a wrong value, it is a drift: claudia
// 🎯T112 made the grant opt-in and jevons asked for nothing, so codex
// work seats came up looking healthy with workspace-write and a
// read-only .git. Anything that hands a seat the mode without the grant
// reproduces that, and reproduces it silently.
func TestGitWriteTracksTheSandboxMode(t *testing.T) {
	for _, c := range []struct {
		prov    claudia.Provider
		purpose string
		role    string
	}{
		{claudia.ProviderCodex, claudia.PurposeWork, ""},
		{claudia.ProviderCodex, claudia.PurposeWork, "worker"},
		{claudia.ProviderCodex, claudia.PurposeWork, "auditor"},
		{claudia.ProviderCodex, claudia.PurposeAside, ""},
		{claudia.ProviderCodex, claudia.PurposeOverseer, ""},
		{claudia.ProviderClaude, claudia.PurposeWork, ""},
		{claudia.ProviderGrok, claudia.PurposeWork, ""},
		{claudia.ProviderCursor, claudia.PurposeWork, ""},
	} {
		mode := CodexWorkSandbox(c.prov, c.purpose, c.role)
		granted := CodexWorkGitWrite(c.prov, c.purpose, c.role)
		if (mode == "workspace-write") != granted {
			t.Fatalf("mode=%q but git_write=%v for %v/%s/%s",
				mode, granted, c.prov, c.purpose, c.role)
		}
	}
}

// claudia refuses SandboxGitWrite on a seat whose mode writes nothing
// rather than dropping it, so a grant without workspace-write is not a
// harmless extra — it is a seat that cannot launch at all.
func TestGitWriteNeverWithoutWorkspaceWrite(t *testing.T) {
	for _, c := range []struct {
		name    string
		prov    claudia.Provider
		purpose string
		role    string
	}{
		{"auditor is read-only", claudia.ProviderCodex, claudia.PurposeWork, "auditor"},
		{"aside is not a work seat", claudia.ProviderCodex, claudia.PurposeAside, ""},
		{"overseer is not a work seat", claudia.ProviderCodex, claudia.PurposeOverseer, ""},
		{"claude has no codex sandbox", claudia.ProviderClaude, claudia.PurposeWork, ""},
	} {
		if CodexWorkGitWrite(c.prov, c.purpose, c.role) {
			t.Fatalf("%s: granted a writable .git with mode %q",
				c.name, CodexWorkSandbox(c.prov, c.purpose, c.role))
		}
	}
}

// A codex work seat is granted the write, which is the whole point: it
// commits, and it runs bin/gate -clean, which needs .git/worktrees.
func TestCodexWorkSeatCanWriteItsGit(t *testing.T) {
	if !CodexWorkGitWrite(claudia.ProviderCodex, claudia.PurposeWork, "") {
		t.Fatal("a codex work seat cannot write its .git, so it can neither commit nor gate -clean")
	}
	if !CodexWorkGitWrite(claudia.ProviderCodex, "", "") {
		t.Fatal("an unset purpose mints as work (CodexWorkSandbox) but got no git grant")
	}
}

// 🎯T763's provider-switch strip drops the grant with the mode. Leaving it
// on a switched def is the rehydrate death that strip exists to prevent:
// claudia counts SandboxGitWrite as a requested sandbox policy, so a
// claude seat still carrying it is refused at every launch.
func TestProviderSwitchDropsTheGitGrant(t *testing.T) {
	var sandbox *providerCapField
	for i := range providerCapFields {
		if providerCapFields[i].capability == claudia.CapabilitySandboxPolicy {
			sandbox = &providerCapFields[i]
			break
		}
	}
	if sandbox == nil {
		t.Fatal("no sandbox_policy cap field")
	}
	def := &claudia.AgentDef{
		Provider:             claudia.ProviderCodex,
		Purpose:              claudia.PurposeWork,
		SandboxMode:          "workspace-write",
		SandboxWritableRoots: []string{t.TempDir()},
		SandboxNetworkAccess: true,
		SandboxGitWrite:      true,
	}
	if !sandbox.requested(def) {
		t.Fatal("a def with SandboxGitWrite declares no sandbox policy, so the switch would not strip it")
	}
	if why := sandbox.keep(def); why != "" {
		t.Fatalf("workspace-write must stay droppable: %s", why)
	}
	sandbox.clear(def)
	if def.SandboxGitWrite {
		t.Fatal("the git grant survived a provider switch")
	}
	// A bare grant, with the mode already gone, still reads as a requested
	// policy — otherwise the strip would skip the def and leave it behind.
	if !sandbox.requested(&claudia.AgentDef{SandboxGitWrite: true}) {
		t.Fatal("a bare git grant is invisible to the strip")
	}
}

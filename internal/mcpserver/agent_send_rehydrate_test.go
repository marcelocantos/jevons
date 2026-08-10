// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T409: ensureAgentProcess must perform the same lost-session recovery
// agent_start does (🎯T313), because THIS is the path the impatience
// ladder drives.
//
// Observed 2026-08-10: after a token-exhaustion event, 10 of 16 agents
// carried a session id with no transcript on disk — Materialized is set
// at launch while the file only appears once a session produces a turn.
// registry.Launch fails closed on that, so every repressure hit an
// identical error on a timer, forever, and the fleet could not recover
// without the owner.
//
// This asserts the rotation happens on the send path. It stops short of
// launching a real process: what regressed was the recovery being absent
// here, not the launch itself.
func TestEnsureAgentProcessRotatesPhantomSession(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	// A row that claims a resumable conversation which does not exist.
	phantom := "jv-t372-auto"
	lost := "22fb41fb-29cc-4b70-8b83-8290d757c84f"
	if err := reg.Register(claudia.AgentDef{
		Name:         phantom,
		WorkDir:      t.TempDir(),
		SessionID:    lost,
		Provider:     claudia.ProviderClaude,
		Materialized: true,
		Purpose:      claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}

	// The launch itself is incidental — and may even succeed, since a
	// claude binary is usually on PATH — so stop whatever it started.
	defer reg.Stop(phantom)
	_, _, _ = s.ensureAgentProcess(phantom)

	def := reg.Def(phantom)
	if def == nil {
		t.Fatal("agent vanished from the registry")
	}
	if def.SessionID == lost {
		t.Error("still pinned to the phantom session; every repressure will fail identically forever")
	}
	// Materialized is deliberately NOT asserted here. claudia's
	// Registry.Launch sets it as soon as Start returns a process, so it is
	// true again after any successful launch regardless of whether a
	// conversation exists. That is the ROOT cause of 🎯T409 and it lives in
	// claudia, not here; this test pins the jevons half — that the send
	// path rotates off an unresumable session instead of dead-ending.
	// Identity must survive the rotation, or recovery silently orphans work.
	if def.Purpose != claudia.PurposeWork || def.Name != phantom {
		t.Errorf("rotation lost identity: %+v", def)
	}
}

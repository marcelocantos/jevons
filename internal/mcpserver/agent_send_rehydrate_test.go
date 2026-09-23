// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
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

// A Cursor row reloaded after a bounce RequireResumes its persisted session
// id even when that id never wrote store.db. session/load then returns
// Invalid params and Launch latches the refusal. The send path used to
// return that latch forever. It must rotate once and launch the fresh id.
func TestEnsureAgentProcessRemintsCursorResumeDenial(t *testing.T) {
	const oldSession = "82615afb-238d-44bd-a9fa-407b43009f13"
	path := filepath.Join(t.TempDir(), "agents.json")
	seed, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Register(claudia.AgentDef{
		Name: "multimaze2-po", WorkDir: t.TempDir(), SessionID: oldSession,
		Provider: claudia.ProviderCursor, Purpose: claudia.PurposeWork,
		AutoStart: true, TermLogPath: "-",
	}); err != nil {
		t.Fatal(err)
	}

	// A registry opened on the same file is a bounce: the session id was
	// not minted here, so Launch passes RequireResume.
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var resumes, mints int
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if cfg.RequireResume {
				resumes++
				return nil, fmt.Errorf("acp session/load %s: Invalid params (%w)", cfg.SessionID, claudia.ErrCursorResumeDenied)
			}
			mints++
			return claudia.StartStub(ctx, cfg, nil)
		},
	})

	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	t.Cleanup(reg.StopAll)

	proc, rehydrated, err := s.ensureAgentProcess("multimaze2-po")
	if err != nil {
		t.Fatal(err)
	}
	if proc == nil || !rehydrated {
		t.Fatalf("proc=%v rehydrated=%v", proc != nil, rehydrated)
	}
	if resumes != 1 || mints != 1 {
		t.Fatalf("resumes=%d mints=%d, want one refusal then one fresh mint", resumes, mints)
	}
	if got := reg.Def("multimaze2-po").SessionID; got == oldSession || got == "" {
		t.Fatalf("session = %q, still the id Cursor will not load", got)
	}
}

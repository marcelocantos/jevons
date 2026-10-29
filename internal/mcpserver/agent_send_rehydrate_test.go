// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

var errT756HermeticLaunch = errors.New("hermetic launch seam (T756)")

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
// Missing JSONL is not canonical generation loss. The send path must
// return the launch error and keep the session id. It must not mint a
// replacement on the strength of a missing vendor file.
//
// 🎯T756: the launch seam is stubbed (as in TestEnsureAgentProcessRemintsCursorResumeDenial)
// so this path cannot mint a live Claude seat on the production claudia socket.
// A mutation that bypasses SetLaunchers and hits the real Start path fails
// here because the error will not carry errT756HermeticLaunch.
func TestEnsureAgentProcessRotatesPhantomSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // Claude JSONL under HOME never sees the lost id.

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var starts int
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			return nil, errT756HermeticLaunch
		},
	})

	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	t.Cleanup(reg.StopAll)

	// A row that claims a resumable conversation which does not exist.
	phantom := "jv-t756-phantom"
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

	_, _, err = s.ensureAgentProcess(phantom)
	if err == nil {
		t.Fatal("phantom resume returned success; a missing vendor file must not launch a replacement")
	}
	if starts != 1 {
		t.Fatalf("starts=%d, want exactly one stub launch (no live Claude mint)", starts)
	}
	if !strings.Contains(err.Error(), errT756HermeticLaunch.Error()) {
		t.Fatalf("launch did not use hermetic seam: %v", err)
	}

	def := reg.Def(phantom)
	if def == nil {
		t.Fatal("agent vanished from the registry")
	}
	if def.SessionID != lost || !def.Materialized {
		t.Errorf("session moved off the refused generation: sid=%s mat=%v", def.SessionID, def.Materialized)
	}
	if def.Purpose != claudia.PurposeWork || def.Name != phantom {
		t.Errorf("refusal lost identity: %+v", def)
	}
}

// A Cursor row reloaded after a bounce RequireResumes its persisted session
// id even when that id never wrote store.db. session/load then returns
// Invalid params. Refusal prose is not generation loss: the send path
// tries that resume once, returns the error, and keeps the session id.
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
	if err == nil {
		t.Fatal("cursor resume denial returned success; refusal prose must not mint a replacement")
	}
	if proc != nil || rehydrated {
		t.Fatalf("proc=%v rehydrated=%v; denial must not launch a fresh id", proc != nil, rehydrated)
	}
	if resumes != 1 || mints != 0 {
		t.Fatalf("resumes=%d mints=%d, want one refused resume and no remint", resumes, mints)
	}
	if got := reg.Def("multimaze2-po").SessionID; got != oldSession {
		t.Fatalf("session = %q, want the refused id %q preserved", got, oldSession)
	}
}

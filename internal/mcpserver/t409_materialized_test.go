// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T409 — Materialized means a conversation exists, not that a launch was
// attempted. Recovery is cold-start / explicit recover (RehydrateLostSession),
// never PrepareCompaction / T285 unless the owner is switching providers.

func TestT409TurnBeganPromotesMaterializedForGrok(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	const name = "jv-t409-grok-attest"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: "grok-sess-1",
		Provider: claudia.ProviderGrok, Purpose: claudia.PurposeWork,
		// Materialized false after bare Launch (claudia T15).
	}); err != nil {
		t.Fatal(err)
	}
	if reg.Def(name).Materialized {
		t.Fatal("fixture: Materialized already true before any turn")
	}

	s.markAgentTurnBegan(name)

	if !reg.Def(name).Materialized {
		t.Fatal("Grok turn begin did not MarkMaterialized — host attestation missing")
	}
	if !s.agentHasTurnBegan(name) {
		t.Fatal("process-local turn evidence missing")
	}
}

func TestT409ClaudeTurnBeganWithoutJSONLDoesNotLie(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	const name = "jv-t409-claude-no-jsonl"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: t.TempDir(), SessionID: "claude-no-jsonl-yet",
		Provider: claudia.ProviderClaude, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}

	s.markAgentTurnBegan(name)

	// Claude MarkMaterialized requires durable JSONL — without it the flag
	// stays false. Process-local turn evidence is still recorded (T305).
	if reg.Def(name).Materialized {
		t.Fatal("Claude Materialized set without JSONL — would RequireResume the void")
	}
	if !s.agentHasTurnBegan(name) {
		t.Fatal("turn began should still be recorded process-locally")
	}
}

// Token-exhaustion scale shape: many Materialized+missing rows recover via
// the shared helper rather than dead-ending on identical Launch errors.
func TestT409TokenExhaustionScaleRecoversAll(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}

	type seat struct {
		name, lost string
	}
	seats := []seat{
		{"jv-t409-scale-a", "aaaaaaaa-1111-1111-1111-111111111111"},
		{"jv-t409-scale-b", "bbbbbbbb-2222-2222-2222-222222222222"},
		{"jv-t409-scale-c", "cccccccc-3333-3333-3333-333333333333"},
	}
	for _, se := range seats {
		if err := reg.Register(claudia.AgentDef{
			Name: se.name, WorkDir: t.TempDir(), SessionID: se.lost,
			Provider: claudia.ProviderClaude, Materialized: true,
			Purpose: claudia.PurposeWork, Parent: "jevons-po",
		}); err != nil {
			t.Fatal(err)
		}
	}

	for _, se := range seats {
		lost, ok, err := fleet.RehydrateLostSessionIn(reg, se.name)
		if err != nil || !ok {
			t.Fatalf("%s: recover ok=%v err=%v", se.name, ok, err)
		}
		def := reg.Def(se.name)
		if def.SessionID == se.lost || def.Materialized {
			t.Fatalf("%s still phantom: sid=%s mat=%v (reported new=%s)",
				se.name, def.SessionID, def.Materialized, lost.NewSession)
		}
		if fleet.SessionLost(def) {
			t.Fatalf("%s still SessionLost after recover", se.name)
		}
	}
}

func TestT409RecoverIsNotPrepareCompaction(t *testing.T) {
	// Compile-time / behavioural pin: lost-session recovery is RehydratedDef
	// (cold rotate), and PrepareCompaction stays withdrawn (🎯T40.2).
	f := &fleet.Claudia{}
	if _, err := f.PrepareCompaction("anyone", true); err == nil {
		t.Fatal("PrepareCompaction succeeded — must stay withdrawn for lost-session")
	}
}

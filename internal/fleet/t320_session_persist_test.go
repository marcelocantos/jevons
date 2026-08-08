// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T320 acceptance 3 — never-written seat is not a RequireResume dead-end.
//
// Pre-claudia-c7ba363, Launch set Materialized=true on bare Start. A seat
// killed before the first completed turn had Materialized + no JSONL, so
// SessionLost fired and every later start either hard-failed or rotated via
// T313 rehydrate. After the claudia fix, bare Start leaves Materialized
// false: SessionLost is false, T313 does not engage, and the next Launch
// mints with RequireResume=false (covered hermetically in claudia:
// TestHermeticLaunchDoesNotMaterializeWithoutJSONL).
//
// This file guards the jevons-side half of that contract: the registry
// shape left by "started, no completed turn" must not look like genuine
// session loss to the rehydrate arm.

func TestT320NeverWrittenDoesNotEngageRehydrate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workDir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Shape after Launch without a completed turn (post-c7ba363).
	def := claudia.AgentDef{
		Name:         "jv-t320-never-wrote",
		WorkDir:      workDir,
		SessionID:    "never-wrote-jsonl-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Materialized: false,
		Provider:     claudia.ProviderClaude,
		Parent:       "jevons-po",
		Purpose:      claudia.PurposeWork,
		TargetID:     "T320",
		AutoStart:    true,
	}
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}

	if SessionLost(&def) {
		t.Fatal("never-written (Materialized=false) reported SessionLost — would force T313")
	}

	lost, ok, err := RehydrateLostSessionIn(reg, def.Name)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if ok {
		t.Fatalf("T313 rehydrate engaged for never-written seat (new=%s) — plain resume path broken", lost.NewSession)
	}
	after := reg.Def(def.Name)
	if after == nil || after.SessionID != def.SessionID {
		t.Fatalf("session rotated without loss: before=%s after=%v", def.SessionID, after)
	}
	if after.Materialized {
		t.Fatal("Materialized flipped true without JSONL evidence")
	}
}

// Contrast: genuine Materialized + missing JSONL still engages T313 (residual net).
func TestT320MaterializedMissingJSONLStillRehydrates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workDir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	def := claudia.AgentDef{
		Name:         "jv-t320-genuinely-lost",
		WorkDir:      workDir,
		SessionID:    "lost-with-materialized-aaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Materialized: true,
		Provider:     claudia.ProviderClaude,
		Parent:       "jevons-po",
		Purpose:      claudia.PurposeWork,
		TargetID:     "T320",
	}
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
	if !SessionLost(&def) {
		t.Fatal("Materialized+missing JSONL must still be SessionLost (T313 residual)")
	}
	lost, ok, err := RehydrateLostSessionIn(reg, def.Name)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if !ok {
		t.Fatal("expected T313 rehydrate for genuine loss")
	}
	if lost.OldSession != def.SessionID || lost.NewSession == def.SessionID {
		t.Fatalf("rotation wrong: old=%s new=%s", lost.OldSession, lost.NewSession)
	}
	after := reg.Def(def.Name)
	if after.Materialized || after.SessionID != lost.NewSession {
		t.Fatalf("post-rehydrate row: mat=%v sid=%s", after.Materialized, after.SessionID)
	}
}

// JSONL present (completed-turn evidence) is never SessionLost — plain resume.
func TestT320CompletedTurnJSONLNotLost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := t.TempDir()
	sid := "completed-turn-session-aaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	writeSessionJSONL(t, sid, workDir)

	def := &claudia.AgentDef{
		Name: "ok", WorkDir: workDir, SessionID: sid,
		Materialized: true, Provider: claudia.ProviderClaude,
	}
	if SessionLost(def) {
		t.Fatal("completed-turn JSONL reported lost")
	}
	// Pre-MarkMaterialized but JSONL already on disk: still not lost.
	def.Materialized = false
	if SessionLost(def) {
		t.Fatal("JSONL present with Materialized=false reported lost")
	}
}

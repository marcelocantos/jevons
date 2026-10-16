// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T320: a never-written row does not establish session loss. Historical
// Claude JSONL also cannot determine the state of canonical sidecar history.
// Launch-time generation preservation is covered through LaunchRecovering
// in rehydrate_generation_test.go.

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

	if observedResumeLost(&def) {
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

// An archived transcript also must not trigger replacement of a generation.
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
	if observedResumeLost(def) {
		t.Fatal("completed-turn JSONL reported lost")
	}
	// Pre-MarkMaterialized but JSONL already on disk: still not lost.
	def.Materialized = false
	if observedResumeLost(def) {
		t.Fatal("JSONL present with Materialized=false reported lost")
	}
}

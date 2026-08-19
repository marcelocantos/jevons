// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T528: Session Goal / open-objective Continue stops when the mission is
// evidenced complete (ledger achieve of named TargetIDs, GOAL_STATUS with
// evidence, or answered_or_closed).

func TestT528GoalAllAchievedNoContinueInject(t *testing.T) {
	t.Parallel()
	goal := "SPAWN workers for 🎯T512, T520, and T527; report GOAL_STATUS when done"
	status := map[string]string{
		"T512": "achieved",
		"T520": "achieved",
		"T527": "achieved",
	}
	if fleet.SessionGoalContinueAllowed(goal, status) {
		t.Fatal("Continue must not be allowed when T512+T520+T527 are achieved")
	}
	got := ExtractOpenOwnerIntentWithLedger([]OwnerIntentTurn{
		{Role: "user", Text: goal, TS: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)},
	}, status)
	if got.Recoverable() {
		t.Fatalf("achieved Goal must not recover as open work, text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want residual %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestT528GoalOneIdentifiedContinueStillAllowed(t *testing.T) {
	t.Parallel()
	goal := "SPAWN workers for 🎯T512, T520, and T527"
	status := map[string]string{
		"T512": "achieved",
		"T520": "achieved",
		"T527": "identified",
	}
	if !fleet.SessionGoalContinueAllowed(goal, status) {
		t.Fatal("Continue must stay allowed while T527 is identified")
	}
	got := ExtractOpenOwnerIntentWithLedger([]OwnerIntentTurn{
		{Role: "user", Text: goal},
	}, status)
	if !got.Recoverable() {
		t.Fatalf("open Goal must still recover, residual=%q", got.Residual)
	}
	if got.Residual != "" {
		t.Fatalf("want empty residual, got %q", got.Residual)
	}
}

func TestT528GoalStatusCompleteWithEvidenceCloses(t *testing.T) {
	t.Parallel()
	goal := "SPAWN T512 T520 T527 remint mission"
	turns := []OwnerIntentTurn{
		{Role: "user", Text: goal},
		{
			Role: "assistant",
			Text: "T512 T520 T527 achieved; SHA abcdef0123456789abcd and GATE green.\nGOAL_STATUS: complete",
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("GOAL_STATUS complete with evidence must close, text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestT528EffectiveSessionGoalClearsWhenLedgerComplete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := []byte(`targets:
  T512:
    name: a
    status: achieved
  T520:
    name: b
    status: achieved
  T527:
    name: c
    status: achieved
`)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	def := &claudia.AgentDef{
		Name:    "jevons-po",
		WorkDir: dir,
		Goal:    "SPAWN T512 T520 T527 until complete",
		Purpose: claudia.PurposeWork,
	}
	if g := effectiveSessionGoal(def); g != "" {
		t.Fatalf("effectiveSessionGoal=%q, want empty when all achieved", g)
	}
	cfg := startConfigFromDef(def)
	if cfg.Goal != "" {
		t.Fatalf("Launch Config.Goal=%q, want empty (no Continue inject)", cfg.Goal)
	}
}

func TestT528EffectiveSessionGoalKeepsOpenTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := []byte(`targets:
  T512:
    name: a
    status: achieved
  T527:
    name: c
    status: identified
`)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	def := &claudia.AgentDef{
		Name:    "jevons-po",
		WorkDir: dir,
		Goal:    "SPAWN T512 and T527",
		Purpose: claudia.PurposeWork,
	}
	if g := effectiveSessionGoal(def); g != def.Goal {
		t.Fatalf("effectiveSessionGoal=%q, want Goal kept while T527 identified", g)
	}
}

func TestT528LoadOpenOwnerIntentWithLedger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := []byte(`targets:
  T512:
    name: a
    status: achieved
  T520:
    name: b
    status: achieved
  T527:
    name: c
    status: achieved
`)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	chatDir := filepath.Join(state, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","timestamp":"2026-08-19T12:00:00Z","message":{"role":"user","content":"SPAWN T512 T520 T527 remint"}}` + "\n"
	if err := os.WriteFile(filepath.Join(chatDir, "jevons.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntentWithLedger(state, "jevons", dir)
	if got.Recoverable() {
		t.Fatalf("ledger-complete Goal must not resume, text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

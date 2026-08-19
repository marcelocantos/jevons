// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
)

// TestT474ReapDefersWhileRotationPending pins the interleaving half of
// 🎯T474: reap_done must not Remove a name that still has an undelivered
// handover. That Remove is what let ensureRegistered's MINT branch invent
// an aside ghost after compaction/migration prepared a successor.
func TestT474ReapDefersWhileRotationPending(t *testing.T) {
	const name = "jv-t444-phase-remint"
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: "/work/jevons", SessionID: "prepared",
		Provider: claudia.ProviderGrok, Purpose: claudia.PurposeWork,
		Parent: "jevons-po", TargetID: "T444",
	}); err != nil {
		t.Fatal(err)
	}
	led := &sweepLedger{pending: []handover.Pending{{
		Agent: name, Purpose: claudia.PurposeWork, WorkDir: "/work/jevons",
		NewSessionID: "prepared", Parent: "jevons-po", TargetID: "T444",
		TranscriptPath: "/t.jsonl",
	}}}
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := &Server{registry: reg, migrator: led}
	s.maybeReapDoneWorkAgent(name, "Done. Oracle: go test ./internal/fleet -run T474 green.")

	if reg.Def(name) == nil {
		t.Fatal("reap_done removed the agent while a rotation was pending")
	}
	got := findLifecycle(cap.records, compAgentLifecycle, "reap_done")
	if got == nil {
		t.Fatal("expected reap_done skipped lifecycle record")
	}
	if got["outcome"] != "skipped" || got["reason"] != "rotation_pending" {
		t.Fatalf("outcome=%v reason=%v want skipped/rotation_pending", got["outcome"], got["reason"])
	}

	// Control: once the seed is delivered, a finish report reaps normally.
	led.pending[0].Delivered = true
	cap.records = nil
	s.maybeReapDoneWorkAgent(name, "Done.")
	if reg.Def(name) != nil {
		t.Fatal("delivered rotation must not block a genuine finished-work reap")
	}
}

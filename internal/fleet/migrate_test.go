// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/handover"
)

// migrateFixture builds a registry holding one Grok agent whose session
// transcript exists on disk, plus the roots and handover store the
// migration path needs.
func migrateFixture(t *testing.T, sessionID string, withTranscript bool) (*Claudia, *handover.Store, string) {
	t.Helper()
	dir := t.TempDir()
	grokSessions := filepath.Join(dir, "grok-sessions")

	transcript := ""
	if withTranscript {
		bucket := filepath.Join(grokSessions, discovery.EncodeCWDBucket("/work/repo"), sessionID)
		if err := os.MkdirAll(bucket, 0o755); err != nil {
			t.Fatal(err)
		}
		transcript = filepath.Join(bucket, "chat_history.jsonl")
		if err := os.WriteFile(transcript, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: "/work/repo", SessionID: sessionID,
		Provider: claudia.ProviderGrok, Materialized: true, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}

	store := handover.NewStore(filepath.Join(dir, "handover"))
	f := NewClaudia(reg)
	f.SetSessionRoots(discovery.Roots{GrokSessions: grokSessions})
	f.SetHandoverStore(store)
	return f, store, transcript
}

func TestT543ThrowawayCompactIsNotAWorkSeat(t *testing.T) {
	got := throwawayCompactDef(claudia.AgentDef{
		Name: "worker", Purpose: claudia.PurposeWork, TargetID: "T543",
		AutoStart: true, Materialized: true, ConnectURL: "http://old", ConnectPID: 42,
	}, "jv-compact-12345678", "compact-session", claudia.ProviderCodex)
	if got.Purpose == claudia.PurposeWork || got.Purpose != claudia.PurposeAside {
		t.Fatalf("purpose=%q; want aside, never work", got.Purpose)
	}
	if got.TargetID != "" {
		t.Fatalf("target_id=%q; want empty", got.TargetID)
	}
	if got.AutoStart || got.Materialized || got.ConnectURL != "" || got.ConnectPID != 0 {
		t.Fatalf("throwaway retained work lifecycle state: %+v", got)
	}
}

func TestT543CompleteThinBriefMintsCompactOnce(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e54"
	f, _, _ := migrateFixture(t, oldSession, false)
	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderCodex, true)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if !handover.DistillTooThin(pending.Brief) && !handover.DistillTooThin(handover.Distill(pending.TranscriptPath)) {
		t.Fatalf("fixture is not DistillTooThin: source=%q brief=%q", pending.BriefSource, pending.Brief)
	}
	mints := 0
	f.compactBrief = func(handover.Pending) (string, string, error) {
		mints++
		return "compact-sess-t543", "in flight: T543 still open", nil
	}
	if _, err := f.CompleteThinBrief(pending); err != nil {
		t.Fatalf("CompleteThinBrief: %v", err)
	}
	saved, ok, err := f.PendingHandover("jevons-po")
	if err != nil || !ok {
		t.Fatalf("pending missing: ok=%v err=%v", ok, err)
	}
	if _, err := f.CompleteThinBrief(saved); err != nil {
		t.Fatalf("second CompleteThinBrief: %v", err)
	}
	if mints != 1 {
		t.Fatalf("compact mints=%d; DistillTooThin must launch throwaway compact once", mints)
	}
}

// TestPrepareMigrationPersistsPointerBeforeRotating is the ordering
// oracle for 🎯T285: rotation overwrites the session id, so unless the
// transcript pointer is already on disk the successor can never be told
// where it came from. Asserting on the STORE (not the return value)
// proves the durable half.
func TestPrepareMigrationPersistsPointerBeforeRotating(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e21"
	f, store, transcript := migrateFixture(t, oldSession, true)

	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if pending.TranscriptPath != transcript {
		t.Errorf("pointer = %q, want %q", pending.TranscriptPath, transcript)
	}

	saved, ok, err := store.Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("nothing persisted: ok=%v err=%v", ok, err)
	}
	if saved.TranscriptPath != transcript || saved.OldSessionID != oldSession {
		t.Fatalf("persisted record lost the pointer: %+v", saved)
	}
	if saved.From != string(claudia.ProviderGrok) || saved.To != string(claudia.ProviderClaude) {
		t.Errorf("persisted providers wrong: %+v", saved)
	}

	// The row is rotated: new provider, NEW session, and not a resume —
	// claudia would fail closed trying to resume a Grok id as Claude.
	def := f.reg.Def("jevons-po")
	if def == nil {
		t.Fatal("agent vanished from the registry")
	}
	if def.Provider != claudia.ProviderClaude {
		t.Errorf("provider = %s, want claude", def.Provider)
	}
	if def.SessionID == oldSession || def.SessionID == "" {
		t.Errorf("session not rotated: %q", def.SessionID)
	}
	if def.Materialized {
		t.Error("rotated row still marked Materialized — launch would demand a resume")
	}
	if def.WorkDir != "/work/repo" || def.Purpose != claudia.PurposeWork {
		t.Errorf("rotation lost row fields: %+v", def)
	}
}

func TestPrepareMigrationKeepsGoal(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e99"
	f, _, _ := migrateFixture(t, oldSession, true)
	def := f.reg.Def("jevons-po")
	def.Goal = "Achieve 🎯T510"
	if err := f.reg.Register(*def); err != nil {
		t.Fatal(err)
	}
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderCodex, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	got := f.reg.Def("jevons-po")
	if got == nil || got.Goal != "Achieve 🎯T510" {
		t.Fatalf("Goal after Grok→Codex remint = %+v", got)
	}
	if got.Provider != claudia.ProviderCodex {
		t.Fatalf("provider = %q", got.Provider)
	}
}

// 🎯T324: migrate claude→grok with prior model=fable never leaves fable under
// grok — binding is rewritten to the new provider default (or empty when
// none). Session-truth, not fail-closed sniff.
func TestPrepareMigrationClearsModelPin(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e26"
	f, _, _ := migrateFixture(t, oldSession, true)
	// Stamp a Claude-family pin on the pre-migrate Grok→Claude path's
	// counterpart: start on Claude with Model=fable, migrate to Grok.
	if err := f.reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: "/work/repo", SessionID: oldSession,
		Provider: claudia.ProviderClaude, Materialized: true,
		Purpose: claudia.PurposeWork, Model: "fable",
	}); err != nil {
		t.Fatal(err)
	}
	// Fixture roots only know Grok sessions; force the cold switch so we
	// exercise pin rewrite without a Claude transcript on disk.
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderGrok, true); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	def := f.reg.Def("jevons-po")
	if def == nil {
		t.Fatal("agent vanished")
	}
	if def.Provider != claudia.ProviderGrok {
		t.Fatalf("provider=%s want grok", def.Provider)
	}
	if def.Model == "fable" || strings.Contains(strings.ToLower(def.Model), "fable") {
		t.Fatalf("Model pin survived migrate: %q — Anthropic residue under Grok", def.Model)
	}
	// Bound to Grok default (condensable), not left as bare empty forever.
	if def.Model != cli.DefaultGrokModel {
		t.Fatalf("Model after migrate=%q want provider default %q", def.Model, cli.DefaultGrokModel)
	}
}

// TestPrepareMigrationRefusesWhenHistoryCannotBeHandedOver: no transcript
// means a silent cold start, which is the outcome this path exists to
// prevent — so it refuses, and leaves the agent untouched.
func TestPrepareMigrationRefusesWhenHistoryCannotBeHandedOver(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e22"
	f, store, _ := migrateFixture(t, oldSession, false)

	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err == nil {
		t.Fatal("migration proceeded with no transcript to hand over")
	}
	def := f.reg.Def("jevons-po")
	if def.Provider != claudia.ProviderGrok || def.SessionID != oldSession {
		t.Fatalf("refused migration still mutated the row: %+v", def)
	}
	if _, ok, _ := store.Get("jevons-po"); ok {
		t.Error("refused migration left a pending record")
	}

	// force is the deliberate cold switch: it proceeds and says so.
	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, true)
	if err != nil {
		t.Fatalf("forced migration: %v", err)
	}
	if pending.Usable() {
		t.Error("cold switch produced a usable handover")
	}
	if got := pending.Describe(); !strings.Contains(got, "COLD") {
		t.Errorf("forced migration does not admit the cold start: %q", got)
	}
	if f.reg.Def("jevons-po").Provider != claudia.ProviderClaude {
		t.Error("forced migration did not rotate the row")
	}
}

// TestPrepareMigrationRejectsNoOpAndUnknownAgents.
func TestPrepareMigrationRejectsNoOpAndUnknownAgents(t *testing.T) {
	f, _, _ := migrateFixture(t, "019fd13d-e500-7913-b96c-981e50aa2e23", true)

	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderGrok, false); err == nil {
		t.Error("migrating to the provider already in use was accepted")
	}
	if _, err := f.PrepareMigration("nobody", claudia.ProviderClaude, false); err == nil {
		t.Error("migrating an unknown agent was accepted")
	}
	if _, err := f.PrepareMigration("jevons-po", "", false); err == nil {
		t.Error("empty target provider was accepted")
	}
}

// TestSeedSuccessorWithoutPendingIsQuiet: the normal case — an agent that
// did not just migrate is not seeded, and that is not an error.
func TestSeedSuccessorWithoutPendingIsQuiet(t *testing.T) {
	f, _, _ := migrateFixture(t, "019fd13d-e500-7913-b96c-981e50aa2e24", true)
	if _, ok, err := f.SeedSuccessor("jevons-po"); err != nil || ok {
		t.Fatalf("SeedSuccessor on a non-migrating agent: ok=%v err=%v", ok, err)
	}
}

// TestSeedSuccessorKeepsRecordWhenNoProcess: a failed launch must leave
// the handover pending so the next launch delivers it, rather than
// consuming it against a dead agent.
func TestSeedSuccessorKeepsRecordWhenNoProcess(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e25"
	f, store, _ := migrateFixture(t, oldSession, true)
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}

	// No process was launched (the fixture never starts one).
	if _, ok, err := f.SeedSuccessor("jevons-po"); ok || err == nil {
		t.Fatalf("seeding without a live process: ok=%v err=%v", ok, err)
	}
	saved, ok, err := store.Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("record lost after failed seed: ok=%v err=%v", ok, err)
	}
	if !saved.Usable() {
		t.Fatal("record marked delivered despite no successor receiving it")
	}
}

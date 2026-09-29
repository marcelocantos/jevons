// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
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
		transcript = filepath.Join(bucket, "updates.jsonl")
		if err := os.WriteFile(transcript, []byte(`{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hello"}}}}`+"\n"), 0o644); err != nil {
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
	f.migrationTransfer = func(args MigrationTransferArgs) (MigrationTransferResult, error) {
		return MigrationTransferResult{Brief: "In-flight work: " + args.Goal + "\nRecent context: " + args.Transcript}, nil
	}
	f.stoppedMigrate = func(name string, args claudia.MigrateArgs, history string) (StoppedMigration, error) {
		source := reg.Def(name)
		if source == nil {
			return StoppedMigration{}, fmt.Errorf("missing fixture seat %s", name)
		}
		if history == "" && !args.Force {
			return StoppedMigration{}, fmt.Errorf("no predecessor context")
		}
		if history == "" {
			history = "system: forced cold start; no predecessor turns were retained"
		}
		transfer, err := f.migrationTransfer(MigrationTransferArgs{
			Destination: args.Provider, Goal: source.Goal, Transcript: history,
		})
		if err != nil {
			return StoppedMigration{}, err
		}
		if strings.TrimSpace(transfer.Brief) == "" {
			return StoppedMigration{}, fmt.Errorf("empty transfer brief")
		}
		next := *source
		next.Provider = cli.SubscriptionSeatProvider(args.Provider)
		next.Model = args.Model
		next.SessionID = uuid.NewString()
		next.Materialized = false
		if err := reg.Register(next); err != nil {
			return StoppedMigration{}, err
		}
		return StoppedMigration{Source: *source, Destination: next, Transfer: transfer}, nil
	}
	return f, store, transcript
}

func TestSeatTranscriptReadsSpoolNotVendor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JEVONS_SPOOL_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-25.log"), []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"jv-cursor","type":"text","text":"from spool"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	sid := "00000000-0000-4000-8000-000000000866"
	projects := filepath.Join(dir, "projects")
	if err := os.MkdirAll(filepath.Join(projects, "bucket"), 0o755); err != nil {
		t.Fatal(err)
	}
	vendor := filepath.Join(projects, "bucket", sid+".jsonl")
	if err := os.WriteFile(vendor, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := seatTranscript(claudia.AgentDef{
		Name: "jv-cursor", Provider: claudia.ProviderCursor, SessionID: sid,
	}, discovery.Roots{ClaudeProjects: projects})
	if got == "" || got == vendor {
		t.Fatalf("sidecar handover path = %q (vendor %q)", got, vendor)
	}
	if !strings.Contains(got, ".view") {
		t.Fatalf("want spool view, got %q", got)
	}
}

func TestT543ThrowawayCompactIsNotAWorkSeat(t *testing.T) {
	got, err := throwawayCompactDef(claudia.AgentDef{
		Name: "worker", Purpose: claudia.PurposeWork, TargetID: "T543",
		AutoStart: true, Materialized: true, ConnectURL: "http://old", ConnectPID: 42,
	}, "jv-compact-12345678", "compact-session", claudia.ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
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
	f, store, _ := migrateFixture(t, oldSession, true)
	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderCodex, true)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	mints := 0
	f.compactBrief = func(handover.Pending) (string, string, error) {
		mints++
		return "compact-sess-t543", "in flight: T543 still open", nil
	}
	if _, err := f.CompleteThinBrief(pending); err != nil {
		t.Fatalf("CompleteThinBrief: %v", err)
	}
	if _, err := f.CompleteThinBrief(pending); err != nil {
		t.Fatalf("second CompleteThinBrief: %v", err)
	}
	if mints != 0 {
		t.Fatalf("compact mints=%d; completed Claudia transfer must not run a second summarizer", mints)
	}
	if _, ok, err := store.Get("jevons-po"); err != nil || ok {
		t.Fatalf("Claudia-owned transfer wrote a host handover: ok=%v err=%v", ok, err)
	}
}

// Jevons supplies normalized predecessor history, while Claudia owns the
// persisted destination and handover. The host must not write a second one.
func TestStoppedMigrationDelegatesHandoverWithoutHostLedger(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e21"
	f, store, _ := migrateFixture(t, oldSession, true)

	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if pending.Remap != handover.RemapClaudiaMigrate || !pending.Delivered ||
		!strings.Contains(pending.Brief, "hello") || pending.OldSessionID != oldSession {
		t.Fatalf("Claudia transfer result lost context or source identity: %+v", pending)
	}
	if _, ok, err := store.Get("jevons-po"); err != nil || ok {
		t.Fatalf("stopped migration wrote a Jevons handover: ok=%v err=%v", ok, err)
	}

	// The row is rotated: new provider, NEW session, and not a resume —
	// claudia would fail closed trying to resume a Grok id as Claude.
	def := f.reg.Def("jevons-po")
	if def == nil {
		t.Fatal("agent vanished from the registry")
	}
	if cli.PlanProvider(def.Provider) != claudia.ProviderClaude {
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

func TestStoppedMigrationUsesDurableJournalWhenProviderHasNoTranscript(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e26"
	f, _, _ := migrateFixture(t, oldSession, false)
	def := f.reg.Def("jevons-po")
	def.Provider = claudia.ProviderCodex
	if err := f.reg.Register(*def); err != nil {
		t.Fatal(err)
	}
	f.SetRetainedHistory(func(name string) (string, error) {
		if name != "jevons-po" {
			t.Fatalf("journal lookup for %q", name)
		}
		return "user: Remember AMBERPINE59\nassistant: STORED\n", nil
	})
	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderCursor, false)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if !strings.Contains(pending.Brief, "AMBERPINE59") {
		t.Fatalf("transfer lost retained history: %+v", pending)
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
	if cli.PlanProvider(got.Provider) != claudia.ProviderCodex {
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
	// A migration now requires the predecessor transcript even when forced.
	projects := t.TempDir()
	bucket := filepath.Join(projects, discovery.EncodeCWDBucket("/work/repo"))
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, oldSession+".jsonl"), []byte(`{"type":"user","message":{"role":"user","content":"continue"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.SetSessionRoots(discovery.Roots{ClaudeProjects: projects})
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderGrok, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	def := f.reg.Def("jevons-po")
	if def == nil {
		t.Fatal("agent vanished")
	}
	if cli.PlanProvider(def.Provider) != claudia.ProviderGrok {
		t.Fatalf("provider=%s want grok", def.Provider)
	}
	if def.Model == "fable" || strings.Contains(strings.ToLower(def.Model), "fable") {
		t.Fatalf("Model pin survived migrate: %q — Anthropic residue under Grok", def.Model)
	}
	// The sidecar resolves its own default; no stale source pin may remain.
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

	// Force may interrupt a turn, but cannot bypass the transfer step.
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, true); err == nil {
		t.Fatal("forced migration bypassed the required transfer")
	}
	if f.reg.Def("jevons-po").Provider != claudia.ProviderGrok {
		t.Error("failed forced migration rotated the row")
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

// Claudia delivers the stopped-seat seed inside its registry operation;
// Jevons' old SeedSuccessor path must remain inert.
func TestStoppedMigrationNeedsNoHostSeed(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e25"
	f, store, _ := migrateFixture(t, oldSession, true)
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}

	if _, ok, err := f.SeedSuccessor("jevons-po"); ok || err != nil {
		t.Fatalf("host attempted a second seed: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.Get("jevons-po"); err != nil || ok {
		t.Fatalf("host kept a second handover: ok=%v err=%v", ok, err)
	}
}

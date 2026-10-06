// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/handover"
)

// Since 🎯T1013.5, PrepareMigrationPinned is a thin wrapper over
// [claudia.Registry.Migrate]: the live-vs-stopped dispatch, the
// already-on-provider refusal, the turn-in-flight refusal and
// force-interrupt-then-retry, and the pending-destination retry all live
// in Claudia and are covered there (registry_migrate_seat_test.go). These
// tests cover what the wrapper itself still owns: the 🎯T763
// capability-drop refusal (checked before Claudia is asked to do
// anything), best-effort retained-transcript plumbing, and translating
// Claudia's result (or refusal) into the [handover.Pending] shape the
// mcpserver/server callers already understand.

// migrateStoppedFixture registers a non-live Grok seat and wires a
// registry whose Launch goes through [claudia.StartStub] instead of a
// real provider process.
func migrateStoppedFixture(t *testing.T, sessionID string) (*Claudia, *claudia.Registry) {
	t.Helper()
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Adopt: func(claudia.Config) (*claudia.Agent, error) { return nil, claudia.ErrNoSessionWindow },
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if cfg.AdoptOnly {
				return nil, claudia.ErrNoSessionWindow
			}
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	reg.SetMigrationSummarizer(func(_ context.Context, a claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
		return claudia.MigrationTransferResult{Brief: "In-flight work: " + a.Goal + "\nRecent context: " + a.Transcript}, nil
	})
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: "/work/repo", SessionID: sessionID,
		Provider: claudia.ProviderGrok, Materialized: true, Purpose: claudia.PurposeWork,
		Goal: "Achieve 🎯T1013.5",
	}); err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	store := handover.NewStore(filepath.Join(dir, "handover"))
	f.SetHandoverStore(store)
	return f, reg
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

// TestPrepareMigrationRejectsNoOpAndUnknownAgents: the wrapper's own input
// checks (unknown agent, empty target) and Claudia's already-on-provider
// refusal, all without needing a live or stub process.
func TestPrepareMigrationRejectsNoOpAndUnknownAgents(t *testing.T) {
	f, _ := migrateStoppedFixture(t, "019fd13d-e500-7913-b96c-981e50aa2e23")

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

// TestPrepareMigrationRefusesCapabilityDrop (🎯T763): the host-layer
// refusal runs before Claudia touches the seat at all — a switch that
// would drop a restriction the destination provider cannot enforce is
// refused, and the row stays untouched.
func TestPrepareMigrationRefusesCapabilityDrop(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	var started []claudia.Config
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			started = append(started, cfg)
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	const name = "jevons-po"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "t763-codex-sid", Provider: claudia.ProviderCodex,
		SandboxMode: "workspace-write", SandboxWritableRoots: []string{dir},
		SandboxNetworkAccess: true, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)

	if _, err := f.PrepareMigration(name, claudia.ProviderClaude, true); err == nil {
		t.Fatal("capability-dropping migrate was accepted")
	}
	if len(started) != 0 {
		t.Fatalf("Claudia was asked to launch before the capability refusal: %+v", started)
	}
	def := reg.Def(name)
	if def.Provider != claudia.ProviderCodex || def.SandboxMode == "" {
		t.Fatalf("refused migrate still mutated the row: %+v", def)
	}
}

// TestStoppedMigrationDelegatesHandoverWithoutHostLedger: a stopped seat
// moves via Claudia's single Migrate call, and the wrapper's result
// carries Claudia's outcome without jevons writing a second, host-owned
// handover record.
func TestStoppedMigrationDelegatesHandoverWithoutHostLedger(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e21"
	f, reg := migrateStoppedFixture(t, oldSession)
	f.SetRetainedHistory(func(name string) (string, error) {
		return "user: continue the migration work\nassistant: on it\n", nil
	})

	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if pending.Remap != handover.RemapClaudiaMigrate || !pending.Delivered ||
		pending.OldSessionID != oldSession {
		t.Fatalf("Claudia transfer result lost context or source identity: %+v", pending)
	}
	if _, ok, err := f.handovers.Get("jevons-po"); err != nil || ok {
		t.Fatalf("stopped migration wrote a jevons handover: ok=%v err=%v", ok, err)
	}
	if _, ok, err := f.SeedSuccessor("jevons-po"); ok || err != nil {
		t.Fatalf("host attempted a second seed: ok=%v err=%v", ok, err)
	}

	def := reg.Def("jevons-po")
	if def == nil {
		t.Fatal("agent vanished from the registry")
	}
	if cli.PlanProvider(def.Provider) != claudia.ProviderClaude {
		t.Errorf("provider = %s, want claude", def.Provider)
	}
	if def.SessionID == oldSession || def.SessionID == "" {
		t.Errorf("session not rotated: %q", def.SessionID)
	}
	if def.WorkDir != "/work/repo" || def.Purpose != claudia.PurposeWork {
		t.Errorf("rotation lost row fields: %+v", def)
	}
	if def.Goal != "Achieve 🎯T1013.5" {
		t.Errorf("Goal after Grok→Claude migrate = %+v", def)
	}
}

// TestStoppedMigrationRefusesColdWithoutForce: Claudia's own refusal for
// "nothing to hand over at all" surfaces through the wrapper unchanged,
// and a refused attempt leaves the row untouched.
func TestStoppedMigrationRefusesColdWithoutForce(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e22"
	f, reg := migrateStoppedFixture(t, oldSession)

	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err == nil {
		t.Fatal("cold migration with no retained history and no force was accepted")
	}
	def := reg.Def("jevons-po")
	if def.Provider != claudia.ProviderGrok || def.SessionID != oldSession {
		t.Fatalf("refused migration still mutated the row: %+v", def)
	}
}

// TestSeedSuccessorWithoutPendingIsQuiet: the normal case — an agent that
// did not just migrate is not seeded, and that is not an error.
func TestSeedSuccessorWithoutPendingIsQuiet(t *testing.T) {
	f, _ := migrateStoppedFixture(t, "019fd13d-e500-7913-b96c-981e50aa2e24")
	if _, ok, err := f.SeedSuccessor("jevons-po"); err != nil || ok {
		t.Fatalf("SeedSuccessor on a non-migrating agent: ok=%v err=%v", ok, err)
	}
}

// TestPrepareMigrationLiveSeatTurnInFlight exercises the live path end to
// end through a [claudia.StartStub] seat: a turn in flight refuses the
// move without force, and a forced call interrupts it and retries once —
// Claudia's mechanics (🎯T1013.5), reached through the jevons wrapper.
func TestPrepareMigrationLiveSeatTurnInFlight(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	inFlight := true
	var interrupted int
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(ctx, cfg, &claudia.StubAgentOps{
				PromptInFlight: func() bool { return inFlight },
				Interrupt: func() error {
					interrupted++
					inFlight = false
					return nil
				},
			})
		},
	})
	reg.SetMigrationSummarizer(func(_ context.Context, a claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
		return claudia.MigrationTransferResult{Brief: "brief for " + string(a.Destination)}, nil
	})
	const name = "jevons-po"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "live-src", Provider: claudia.ProviderClaude,
		Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	proc, err := reg.Launch(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.Stop)
	proc.PublishEvent(claudia.Event{Type: "user", Text: "patch migrate.go"})

	f := NewClaudia(reg)

	if _, err := f.PrepareMigration(name, claudia.ProviderGrok, false); err == nil ||
		!strings.Contains(err.Error(), "turn in flight") {
		t.Fatalf("err = %v, want turn-in-flight refusal", err)
	}
	if interrupted != 0 {
		t.Fatalf("interrupted = %d, want 0 without force", interrupted)
	}
	if def := reg.Def(name); def.Provider != claudia.ProviderClaude {
		t.Fatalf("seat moved despite the turn-in-flight refusal: %+v", def)
	}

	// The forced retry's own context-transfer step needs real provider
	// credentials this hermetic fixture does not have (claudia's live
	// Agent.Migrate summarizer seam is package-private — only
	// Registry.MigrateStopped's summarizer is overridable from outside
	// claudia, see migrateStoppedFixture). What this call site owns, and
	// what stays assertable without those credentials, is that force
	// actually interrupted the in-flight turn and retried past the
	// turn-in-flight refusal rather than stopping at it a second time;
	// claudia's own suite (registry_migrate_seat_test.go,
	// TestRegistryMigrateLiveSeatForceInterruptsThenRetries) covers the
	// retry succeeding end to end.
	_, err = f.PrepareMigration(name, claudia.ProviderGrok, true)
	if err == nil || strings.Contains(err.Error(), "turn in flight") {
		t.Fatalf("forced migrate err = %v, want past the turn-in-flight refusal", err)
	}
	if interrupted != 1 {
		t.Fatalf("interrupted = %d, want exactly 1 for the forced retry", interrupted)
	}
}

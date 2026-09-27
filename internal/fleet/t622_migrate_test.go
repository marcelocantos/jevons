// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
)

func migrateSource(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("migrate.go")
	if err != nil {
		t.Fatalf("read migrate.go: %v", err)
	}
	return string(body)
}

// The capability path must be tried before Stop+Register+Launch (🎯T622).
func TestT622MigratePrefersClaudiaPrimitive(t *testing.T) {
	src := migrateSource(t)
	invoke := strings.Index(src, "live.Migrate(args)")
	remap := strings.Index(src, "remapViaClaudia")
	stop := strings.Index(src, "f.reg.Stop(name)")
	if invoke < 0 {
		t.Fatal("migrate.go does not call live.Migrate — claudia Agent.Migrate exists and must be used (🎯T622)")
	}
	if remap < 0 {
		t.Fatal("PrepareMigration does not call remapViaClaudia")
	}
	if stop < 0 {
		t.Fatal("the rotate fallback is gone; providers without Migrate still need Stop+Register")
	}
	if remap > stop {
		t.Fatalf("remapViaClaudia is after Stop (%d > %d): the destructive path must be the fallback", remap, stop)
	}
}

func TestT622PrepareMigrationInvokesClaudiaMigrate(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa6220"
	f, store, _ := migrateFixture(t, oldSession, true)
	var got *claudia.MigrateArgs
	calls := 0
	f.liveSession = func(string) (string, string) { return "live-successor-session", "" }
	f.liveMigrate = func(args *claudia.MigrateArgs) error {
		calls++
		cp := *args
		got = &cp
		return nil
	}

	pending, err := f.PrepareMigrationPinned("jevons-po", claudia.ProviderClaude, "claude-sonnet-5", false)
	if err != nil {
		t.Fatalf("PrepareMigrationPinned: %v", err)
	}
	if calls != 1 || got == nil {
		t.Fatal("claudia Agent.Migrate was not invoked")
	}
	if got.Provider != claudia.SubscriptionSeatProvider(claudia.ProviderClaude) {
		t.Fatalf("MigrateArgs.Provider=%s", got.Provider)
	}
	if got.Model != "claude-sonnet-5" {
		t.Fatalf("MigrateArgs.Model=%q", got.Model)
	}
	if got.ContextBrief == "" || strings.Contains(got.ContextBrief, "INERT PREDECESSOR") {
		t.Fatalf("work successor did not receive the transfer brief: %q", got.ContextBrief)
	}
	if pending.Remap != handover.RemapClaudiaMigrate {
		t.Fatalf("Remap=%q, want %s", pending.Remap, handover.RemapClaudiaMigrate)
	}
	if !pending.Delivered {
		t.Fatal("live remap must mark the host brief delivered — claudia already sent the inert seed")
	}
	def := f.reg.Def("jevons-po")
	if def == nil {
		t.Fatal("row vanished")
	}
	if def.Provider != claudia.SubscriptionSeatProvider(claudia.ProviderClaude) {
		t.Fatalf("provider=%s", def.Provider)
	}
	if def.SessionID == oldSession {
		t.Fatal("live remap left the predecessor SessionID")
	}
	if !def.Materialized {
		t.Fatal("Materialized=false — that is the rotate fallback, not Agent.Migrate")
	}
	if saved, ok, err := store.Get("jevons-po"); err != nil || ok {
		t.Fatalf("Claudia-owned migration left a host handover: %+v ok=%v err=%v", saved, ok, err)
	}

	mints := 0
	f.compactBrief = func(handover.Pending) (string, string, error) {
		mints++
		return "compact-sess", "should not run", nil
	}
	if _, err := f.CompleteThinBrief(pending); err != nil {
		t.Fatalf("CompleteThinBrief: %v", err)
	}
	if mints != 0 {
		t.Fatal("CompleteThinBrief minted a throwaway compact after claudia remap")
	}
	if _, ok, err := f.SeedSuccessor("jevons-po"); err != nil || ok {
		t.Fatalf("SeedSuccessor after remap: ok=%v err=%v", ok, err)
	}
}

func TestLiveMigrationDelegatesContextTransferToClaudia(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	const sourceSession = "019fd13d-e500-7913-b96c-981e50aa6910"
	const destinationSession = "claudia-live-destination"
	f, store, _ := migrateFixture(t, sourceSession, false)
	f.reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, _ claudia.Config) (*claudia.Agent, error) {
		return claudia.StartStub(t.Context(), claudia.Config{
			Provider: claudia.ProviderGrok, SessionID: sourceSession, WorkDir: t.TempDir(),
		}, nil)
	}})
	live, err := f.reg.Launch("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(live.Stop)
	f.migrationTransfer = func(claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
		t.Fatal("Jevons paid for a context transfer before delegating the live seat")
		return claudia.MigrationTransferResult{}, nil
	}
	f.liveMigrate = func(args *claudia.MigrateArgs) error {
		if args.ContextBrief != "" {
			t.Fatalf("Jevons supplied a second handover brief: %q", args.ContextBrief)
		}
		def := f.reg.Def("jevons-po")
		next := *def
		next.Provider, next.Model, next.SessionID = args.Provider, args.Model, destinationSession
		return f.reg.Register(next)
	}
	f.liveSession = func(string) (string, string) { return destinationSession, "composer-2.5" }
	pending, err := f.PrepareMigrationPinned("jevons-po", claudia.ProviderCursor, "composer-2.5", false)
	if err != nil {
		t.Fatal(err)
	}
	if pending.NewSessionID != destinationSession || pending.Remap != handover.RemapClaudiaMigrate {
		t.Fatalf("Claudia destination was not retained: %+v", pending)
	}
	if _, ok, err := store.Get("jevons-po"); err != nil || ok {
		t.Fatalf("live migration left a host handover: ok=%v err=%v", ok, err)
	}
}

func TestLiveDestinationRetrySkipsSecondTransferSummary(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	const sourceSession = "019fd13d-e500-7913-b96c-981e50aa6229"
	f, _, _ := migrateFixture(t, sourceSession, false)
	def := f.reg.Def("jevons-po")
	def.Materialized = false
	if err := f.reg.Register(*def); err != nil {
		t.Fatal(err)
	}
	f.reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, _ claudia.Config) (*claudia.Agent, error) {
		return claudia.StartStub(t.Context(), claudia.Config{
			Provider:  claudia.SubscriptionSeatProvider(claudia.ProviderCursor),
			SessionID: sourceSession, WorkDir: t.TempDir(),
		}, nil)
	}})
	live, err := f.reg.Launch("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(live.Stop)
	if claudia.PlanProvider(live.Provider()) != claudia.ProviderCursor {
		t.Fatalf("fixture did not create a live destination: %s", live.Provider())
	}
	f.migrationTransfer = func(claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
		t.Fatal("retry paid for a second context-transfer summary")
		return claudia.MigrationTransferResult{}, nil
	}
	f.liveMigrate = func(*claudia.MigrateArgs) error { return fmt.Errorf("Migrate: same provider cursor") }
	f.liveSession = func(string) (string, string) { return "destination-session", "composer-2.5" }
	pending, err := f.PrepareMigrationPinned("jevons-po", claudia.ProviderCursor, "composer-2.5", false)
	if err != nil {
		t.Fatal(err)
	}
	if pending.NewSessionID != "destination-session" ||
		f.reg.Def("jevons-po").SessionID != "destination-session" {
		t.Fatalf("retry did not reconcile the live destination: %+v", pending)
	}
}

func TestMigrationTransferUsesDestinationProviderAndFailureKeepsPredecessor(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa6223"
	f, store, _ := migrateFixture(t, oldSession, true)
	transferCalls, migrateCalls := 0, 0
	f.migrationTransfer = func(args claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
		transferCalls++
		if claudia.PlanProvider(args.Destination) != claudia.ProviderClaude || !strings.Contains(args.Transcript, "hello") {
			t.Fatalf("transfer args = %+v", args)
		}
		return claudia.MigrationTransferResult{Brief: "The work remains open."}, nil
	}
	f.liveMigrate = func(args *claudia.MigrateArgs) error {
		migrateCalls++
		if args.Provider != claudia.SubscriptionSeatProvider(claudia.ProviderClaude) || args.ContextBrief != "The work remains open." {
			t.Fatalf("work successor args = %+v", args)
		}
		return nil
	}
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err != nil {
		t.Fatal(err)
	}
	if transferCalls != 1 || migrateCalls != 1 {
		t.Fatalf("transfer=%d migrate=%d; want one of each", transferCalls, migrateCalls)
	}
	if _, ok, err := store.Get("jevons-po"); err != nil || ok {
		t.Fatalf("live migration wrote a second handover: ok=%v err=%v", ok, err)
	}

	f2, _, _ := migrateFixture(t, oldSession, true)
	f2.migrationTransfer = func(claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
		return claudia.MigrationTransferResult{}, errors.New("summarizer unavailable")
	}
	f2.liveMigrate = func(*claudia.MigrateArgs) error {
		t.Fatal("work successor started after transfer failed")
		return nil
	}
	if _, err := f2.PrepareMigration("jevons-po", claudia.ProviderClaude, true); err == nil {
		t.Fatal("transfer failure was treated as a cold-start migration")
	}
	def := f2.reg.Def("jevons-po")
	if def.Provider != claudia.ProviderGrok || def.SessionID != oldSession {
		t.Fatalf("transfer failure changed predecessor: %+v", def)
	}
}

// TestT646_1RemapDoesNotDeliverHostSeed: after Claudia reports Migrated,
// neither SeedSuccessor nor a direct handOffSeed may Deliver ComposeSeed
// (🎯T646.1).
func TestT646_1RemapDoesNotDeliverHostSeed(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa6461"
	f, store, _ := migrateFixture(t, oldSession, true)
	f.liveMigrate = func(*claudia.MigrateArgs) error { return nil }
	delivers := 0
	f.seedDeliver = func(string, string) (string, error) {
		delivers++
		return "", nil
	}

	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if pending.Remap != handover.RemapClaudiaMigrate {
		t.Fatalf("Remap=%q", pending.Remap)
	}
	if _, ok, err := f.SeedSuccessor("jevons-po"); err != nil || ok {
		t.Fatalf("SeedSuccessor: ok=%v err=%v", ok, err)
	}
	if saved, ok, err := store.Get("jevons-po"); err != nil || ok {
		t.Fatalf("live remap left a host handover: %+v ok=%v err=%v", saved, ok, err)
	}
	f.handOffSeed("jevons-po", pending)
	if delivers != 0 {
		t.Fatalf("host Delivered a continue-seed %d times after Claudia migrate", delivers)
	}
}

func TestT622CapabilityErrorFallsBackToRotate(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa6221"
	f, _, _ := migrateFixture(t, oldSession, true)
	f.liveMigrate = func(*claudia.MigrateArgs) error {
		return &claudia.CapabilityError{
			Provider:   claudia.ProviderGrok,
			Capability: claudia.CapabilityMigrate,
		}
	}
	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatalf("fallback rotate: %v", err)
	}
	if pending.Remap != "" {
		t.Fatalf("Remap=%q after capability fallback", pending.Remap)
	}
	def := f.reg.Def("jevons-po")
	if def == nil || def.Materialized {
		t.Fatalf("rotate fallback must mint an unmaterialized successor: %+v", def)
	}
	if def.Provider != claudia.SubscriptionSeatProvider(claudia.ProviderClaude) {
		t.Fatalf("provider=%s", def.Provider)
	}
}

func TestT622HardFailureDoesNotRotate(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa6222"
	f, store, _ := migrateFixture(t, oldSession, true)
	f.liveMigrate = func(*claudia.MigrateArgs) error {
		return errors.New("turn in flight")
	}
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err == nil {
		t.Fatal("in-flight migrate was treated as success")
	}
	def := f.reg.Def("jevons-po")
	if def.Provider != claudia.ProviderGrok || def.SessionID != oldSession {
		t.Fatalf("hard failure mutated the row: %+v", def)
	}
	if _, ok, _ := store.Get("jevons-po"); ok {
		t.Fatal("hard failure left a pending handover")
	}
}

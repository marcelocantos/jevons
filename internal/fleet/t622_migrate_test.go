// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
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
	if got.Provider != claudia.ProviderClaude {
		t.Fatalf("MigrateArgs.Provider=%s", got.Provider)
	}
	if got.Model != "claude-sonnet-5" {
		t.Fatalf("MigrateArgs.Model=%q", got.Model)
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
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("provider=%s", def.Provider)
	}
	if def.SessionID == oldSession {
		t.Fatal("live remap left the predecessor SessionID")
	}
	if !def.Materialized {
		t.Fatal("Materialized=false — that is the rotate fallback, not Agent.Migrate")
	}
	saved, ok, err := store.Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("handover missing: ok=%v err=%v", ok, err)
	}
	if saved.Remap != handover.RemapClaudiaMigrate || saved.Usable() {
		t.Fatalf("saved remap=%q usable=%v", saved.Remap, saved.Usable())
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
	saved, ok, err := store.Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("handover missing: ok=%v err=%v", ok, err)
	}
	f.handOffSeed("jevons-po", saved)
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
	if def.Provider != claudia.ProviderClaude {
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

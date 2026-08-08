// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/thread"
)

// bareMigrateThread is the exact shape jevons_agent_migrate relaunches a
// rotated agent with (internal/mcpserver/migrate.go): id only, because the
// registry row already holds everything else. Spelled out here so the test
// fails if that call site ever starts carrying more.
func bareMigrateThread(name string) *thread.Thread { return &thread.Thread{ID: name} }

// TestMigrateRelaunchDoesNotStampAsideOnLegacyRow is the 🎯T301 oracle.
//
// A row minted before 🎯T114 carries no purpose at all, and /api/agents
// reads empty as work — so it renders as a product owner. Migration
// relaunches it through a bare thread, and the thread path's "no purpose
// means side chat" default used to be applied to the EXISTING row, not
// just to a mint: the legacy PO was rewritten to aside on disk and the RHS
// flipped it to 💡. Observed 2026-08-08: bullseye-po was the only PO in the
// grok→claude batch whose row had no explicit purpose, and the only one
// that flipped.
//
// Backfilling a default onto a row that predates the field is a guess
// written to durable state; refusing to guess keeps it work.
func TestMigrateRelaunchDoesNotStampAsideOnLegacyRow(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "bullseye-po", WorkDir: "/work/bullseye", SessionID: "legacy-session",
		Provider: claudia.ProviderGrok, Parent: "jevons", Materialized: true,
		// Purpose deliberately absent: a pre-🎯T114 row.
	}); err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	f.SetDefaultProvider(claudia.ProviderGrok)

	if err := f.ensureRegistered(bareMigrateThread("bullseye-po")); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}

	def := reg.Def("bullseye-po")
	if def == nil {
		t.Fatal("agent vanished from the registry")
	}
	if def.Purpose == claudia.PurposeAside {
		t.Fatalf("relaunching a legacy PO through the migrate path stamped purpose=aside; "+
			"it renders as 💡 instead of a product owner (def: %+v)", def)
	}
	if def.Purpose != "" && def.Purpose != claudia.PurposeWork {
		t.Fatalf("purpose = %q, want unchanged (empty) or work", def.Purpose)
	}
	// The relaunch must not have invented the rest of the row either.
	if def.WorkDir != "/work/bullseye" || def.Parent != "jevons" || def.SessionID != "legacy-session" {
		t.Fatalf("relaunch rewrote the row: %+v", def)
	}
}

// TestMigrateRelaunchKeepsExplicitWorkPurpose covers the other half of the
// acceptance: an agent that DOES say purpose=work survives the full
// rotate-then-relaunch sequence as work, not aside.
func TestMigrateRelaunchKeepsExplicitWorkPurpose(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e26"
	f, _, _ := migrateFixture(t, oldSession, true)

	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if err := f.ensureRegistered(bareMigrateThread("jevons-po")); err != nil {
		t.Fatalf("ensureRegistered after rotation: %v", err)
	}
	def := f.reg.Def("jevons-po")
	if def == nil {
		t.Fatal("agent vanished from the registry")
	}
	if def.Purpose != claudia.PurposeWork {
		t.Fatalf("purpose = %q after migrate + relaunch, want work", def.Purpose)
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("provider = %q, want claude", def.Provider)
	}
}

// TestThreadSpawnMintStillDefaultsToAside pins the behaviour the fix must
// NOT take away: a genuine thread spawn with no purpose is a side chat, and
// minting it as aside is correct. The narrowing is to backfill only.
func TestThreadSpawnMintStillDefaultsToAside(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	f.SetDefaultProvider(claudia.ProviderGrok)

	th := &thread.Thread{ID: "att-billing", WorkDir: t.TempDir(), Parent: "jevons"}
	if err := f.ensureRegistered(th); err != nil {
		t.Fatalf("ensureRegistered mint: %v", err)
	}
	if def := reg.Def("att-billing"); def == nil || def.Purpose != claudia.PurposeAside {
		t.Fatalf("thread-path mint should be aside, got %+v", def)
	}
}

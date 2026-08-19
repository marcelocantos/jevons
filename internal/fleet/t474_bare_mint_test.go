// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/thread"
)

// TestT474BareLaunchRecoversWorkIdentityFromHandover is the 🎯T474 mint
// oracle. compactFleetAgent is withdrawn (🎯T40.2); the defect is the MINT
// branch itself when a bare thread.Thread{ID:name} arrives and the row is
// gone. Reconstruct: purpose=work row → rotation persists identity →
// Remove mid-flight → ensureRegistered(bare) must restore purpose/workdir/
// parent/target_id/prepared session — not aside defaults + fresh uuid.
func TestT474BareLaunchRecoversWorkIdentityFromHandover(t *testing.T) {
	const (
		name         = "jv-t444-phase-remint"
		oldSession   = "22dee4dd-old"
		preparedSess = "3b3bdd9e-prepared"
		workdir      = "/Users/marcelo/work/github.com/marcelocantos/jevons"
	)
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workdir, SessionID: oldSession,
		Provider: claudia.ProviderGrok, Model: "grok-4.5",
		Parent: "jevons-po", Purpose: claudia.PurposeWork, TargetID: "T444",
		Materialized: true, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	store := handover.NewStore(filepath.Join(dir, "handover"))
	f := NewClaudia(reg)
	f.SetHandoverStore(store)
	f.SetDefaultProvider(claudia.ProviderGrok)

	// Persist what rotate writes before Register — then delete the row the
	// way reap_done did between "rotation prepared" and Launch.
	if err := store.Put(handover.Pending{
		Agent: name, From: "grok", To: "grok", Kind: handover.KindCompact,
		OldSessionID: oldSession, TranscriptPath: filepath.Join(dir, "t.jsonl"),
		Purpose: claudia.PurposeWork, WorkDir: workdir, Parent: "jevons-po",
		Model: "grok-4.5", TargetID: "T444", NewSessionID: preparedSess,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Remove(name); err != nil {
		t.Fatal(err)
	}
	if reg.Def(name) != nil {
		t.Fatal("precondition: row must be absent before bare Launch")
	}

	if err := f.ensureRegistered(&thread.Thread{ID: name}); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}
	def := reg.Def(name)
	if def == nil {
		t.Fatal("mint did not restore the row")
	}
	if def.Purpose != claudia.PurposeWork {
		t.Fatalf("purpose=%q want work (aside ghost regression)", def.Purpose)
	}
	if def.WorkDir != workdir {
		t.Fatalf("workdir=%q want %q", def.WorkDir, workdir)
	}
	if def.Parent != "jevons-po" {
		t.Fatalf("parent=%q want jevons-po", def.Parent)
	}
	if def.TargetID != "T444" {
		t.Fatalf("target_id=%q want T444", def.TargetID)
	}
	if def.SessionID != preparedSess {
		t.Fatalf("session=%q want prepared %q (discarded rotation successor)", def.SessionID, preparedSess)
	}
}

// TestT474RotatePersistsMintIdentity pins that PrepareMigration writes the
// identity snapshot + prepared session onto the handover before Launch.
func TestT474RotatePersistsMintIdentity(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e44"
	f, store, _ := migrateFixture(t, oldSession, true)
	// Enrich the fixture row with the fields T474 must recover.
	def := f.reg.Def("jevons-po")
	if def == nil {
		t.Fatal("fixture missing")
	}
	next := *def
	next.Parent = "jevons"
	next.TargetID = "T285"
	next.Model = "grok-4.5"
	if err := f.reg.Register(next); err != nil {
		t.Fatal(err)
	}

	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	got, ok, err := store.Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("handover missing on disk: ok=%v err=%v", ok, err)
	}
	if !got.HasMintIdentity() {
		t.Fatalf("handover lacks mint identity: %+v", got)
	}
	if got.Purpose != claudia.PurposeWork || got.Parent != "jevons" || got.TargetID != "T285" {
		t.Fatalf("identity snapshot wrong: %+v", got)
	}
	if got.NewSessionID == "" || got.NewSessionID == oldSession {
		t.Fatalf("NewSessionID=%q want fresh prepared successor", got.NewSessionID)
	}
	if pending.NewSessionID != got.NewSessionID {
		t.Fatalf("return NewSessionID=%q store=%q", pending.NewSessionID, got.NewSessionID)
	}
	row := f.reg.Def("jevons-po")
	if row == nil || row.SessionID != got.NewSessionID {
		t.Fatalf("registry session=%v want %q", row, got.NewSessionID)
	}
}

// TestT474GenuineAsideMintUnchanged is the control: a real thread spawn
// with no pending handover still mints purpose=aside.
func TestT474GenuineAsideMintUnchanged(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := NewClaudia(reg)
	f.SetHandoverStore(handover.NewStore(filepath.Join(t.TempDir(), "handover")))
	f.SetDefaultProvider(claudia.ProviderGrok)

	th := &thread.Thread{ID: "att-billing", WorkDir: t.TempDir(), Parent: "jevons"}
	if err := f.ensureRegistered(th); err != nil {
		t.Fatalf("ensureRegistered: %v", err)
	}
	if def := reg.Def("att-billing"); def == nil || def.Purpose != claudia.PurposeAside {
		t.Fatalf("genuine thread mint must stay aside, got %+v", def)
	}
}

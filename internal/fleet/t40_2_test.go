// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
)

func TestPrepareCompactionWithdrawn(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e40"
	f, reg := migrateStoppedFixture(t, oldSession)
	if _, err := f.PrepareCompaction("jevons-po", true); err == nil {
		t.Fatal("PrepareCompaction succeeded — remint is withdrawn")
	} else if !strings.Contains(err.Error(), "withdrawn") {
		t.Fatalf("PrepareCompaction err=%v want withdrawn", err)
	}
	def := reg.Def("jevons-po")
	if def.SessionID != oldSession || def.Provider != claudia.ProviderGrok {
		t.Fatalf("withdrawn remint mutated the row: %+v", def)
	}
	if _, ok, _ := f.handovers.Get("jevons-po"); ok {
		t.Fatal("withdrawn remint wrote a handover")
	}
}

func TestSeedSuccessorDoesNotInjectCompactSeed(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e44"
	f, reg := migrateStoppedFixture(t, oldSession)
	if err := f.handovers.Put(handover.Pending{
		Agent: "jevons-po", From: "grok", To: "grok",
		Kind: handover.KindCompact, TranscriptPath: "/tmp/does-not-matter",
		OldSessionID: oldSession,
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.SeedSuccessor("jevons-po"); err != nil || ok {
		t.Fatalf("compact SeedSuccessor: ok=%v err=%v; want no seed", ok, err)
	}
	def := reg.Def("jevons-po")
	if def.SessionID != oldSession {
		t.Fatalf("compact seed mutated session: %s", def.SessionID)
	}
}

func TestBounceDoesNotWriteHandover(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: dir, SessionID: "sess-bounce",
		Provider: claudia.ProviderGrok, Materialized: true, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	store := handover.NewStore(filepath.Join(dir, "handover"))
	f := NewClaudia(reg)
	f.SetHandoverStore(store)
	// Bounce is adopt-then-resume, not migrate.
	if _, ok, _ := f.PendingHandover("jevons"); ok {
		t.Fatal("fresh row has a pending handover")
	}
	if _, err := os.Stat(filepath.Join(dir, "handover", "jevons.json")); !os.IsNotExist(err) {
		t.Fatalf("handover file exists after register: %v", err)
	}
}

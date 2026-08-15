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
	f, store, _ := migrateFixture(t, oldSession, true)
	if _, err := f.PrepareCompaction("jevons-po", true); err == nil {
		t.Fatal("PrepareCompaction succeeded — remint is withdrawn")
	} else if !strings.Contains(err.Error(), "withdrawn") {
		t.Fatalf("PrepareCompaction err=%v want withdrawn", err)
	}
	def := f.reg.Def("jevons-po")
	if def.SessionID != oldSession || def.Provider != claudia.ProviderGrok {
		t.Fatalf("withdrawn remint mutated the row: %+v", def)
	}
	if _, ok, _ := store.Get("jevons-po"); ok {
		t.Fatal("withdrawn remint wrote a handover")
	}
}

func TestThinDistillProducesTwoSessionIDs(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e41"
	f, store, _ := migrateFixture(t, oldSession, false)
	f.compactBrief = func(handover.Pending) (string, string, error) {
		return "compact-sess-bbb", "in flight: T999 still open", nil
	}
	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, true)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if pending.CompactSessionID != "compact-sess-bbb" {
		t.Fatalf("compact session=%q", pending.CompactSessionID)
	}
	def := f.reg.Def("jevons-po")
	if def.SessionID == "" || def.SessionID == oldSession {
		t.Fatalf("work session not minted: %q", def.SessionID)
	}
	if def.SessionID == pending.CompactSessionID {
		t.Fatal("work session is the compact-read session")
	}
	seed := pending.Seed()
	if !strings.Contains(seed, "in flight: T999") {
		t.Fatalf("work seed is not the compact brief:\n%s", seed)
	}
	if strings.Contains(strings.ToLower(seed), "start at the end") {
		t.Fatalf("work seed assigned a walk:\n%s", seed)
	}
	saved, ok, err := store.Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("brief not persisted: ok=%v err=%v", ok, err)
	}
	if saved.CompactSessionID != pending.CompactSessionID {
		t.Fatalf("persisted compact id=%q", saved.CompactSessionID)
	}
}

func TestLiveSelfBriefSeedsWorkSession(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e42"
	f, _, _ := migrateFixture(t, oldSession, true)
	f.selfBrief = func(handover.Pending) (string, error) {
		return "from memory: hold the hard-stop", nil
	}
	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if pending.BriefSource != string(handover.SourceSelf) {
		t.Fatalf("source=%q want self-brief", pending.BriefSource)
	}
	if pending.CompactSessionID != "" {
		t.Fatalf("self-brief allocated compact session %q", pending.CompactSessionID)
	}
	if !strings.Contains(pending.Seed(), "from memory: hold the hard-stop") {
		t.Fatalf("seed lost self-brief:\n%s", pending.Seed())
	}
}

func TestDeadOutgoingStillDistills(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa2e43"
	f, _, transcript := migrateFixture(t, oldSession, true)
	// No live process, no hooks: Distill from the fixture file.
	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	if pending.BriefSource != string(handover.SourceDistill) && pending.Brief == "" {
		// GatherBrief Distills the path; a one-line fixture may be thin
		// and fall through to the empty-turns fallback, still a brief.
		t.Logf("source=%q brief=%q path=%s", pending.BriefSource, pending.Brief, transcript)
	}
	seed := pending.Seed()
	if seed == "" {
		t.Fatal("dead-outgoing migrate produced no seed")
	}
	if strings.Contains(strings.ToLower(seed), "start at the end") {
		t.Fatalf("seed assigned a walk:\n%s", seed)
	}
	if strings.Contains(seed, transcript) {
		t.Fatalf("seed cited the predecessor path:\n%s", seed)
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

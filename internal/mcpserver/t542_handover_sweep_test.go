// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/thread"
)

// t542Ledger is a self-contained migrator+handoverLedger so these
// oracles do not depend on T543's sweepLedger fields.
type t542Ledger struct {
	pending  []handover.Pending
	seeded   []string
	cleared  []string
	prepared int
	store    *handover.Store
}

func (l *t542Ledger) PrepareMigration(name string, to claudia.Provider, _ bool) (handover.Pending, error) {
	l.prepared++
	p := handover.Pending{Agent: name, From: "codex", To: string(to), TranscriptPath: "/thin.jsonl"}
	l.pending = append(l.pending, p)
	return p, nil
}
func (l *t542Ledger) CompleteThinBrief(p handover.Pending) (handover.Pending, error) { return p, nil }
func (l *t542Ledger) Launch(*thread.Thread) error                                    { return nil }
func (l *t542Ledger) SeedSuccessor(name string) (handover.Pending, bool, error) {
	l.seeded = append(l.seeded, name)
	return handover.Pending{Agent: name}, true, nil
}
func (l *t542Ledger) PendingHandovers() ([]handover.Pending, error) {
	if l.store != nil {
		return l.store.List()
	}
	return l.pending, nil
}
func (l *t542Ledger) ClearHandover(name string) error {
	l.cleared = append(l.cleared, name)
	if l.store != nil {
		return l.store.Clear(name)
	}
	kept := l.pending[:0]
	for _, p := range l.pending {
		if p.Agent != name {
			kept = append(kept, p)
		}
	}
	l.pending = kept
	return nil
}

func TestT542SweepHandoversReapsColdPending(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t542", SessionID: "s1", Provider: claudia.ProviderCodex,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	led := &t542Ledger{pending: []handover.Pending{{
		Agent: "jv-t542", From: "codex", To: "claude",
		CreatedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}}}
	up := &upward{}
	s := &Server{registry: reg, migrator: led}
	s.SetOverseerDeliver(up.deliver)
	s.SweepHandovers()
	if len(led.cleared) != 1 || led.cleared[0] != "jv-t542" {
		t.Fatalf("cleared = %v; want reap of COLD pending", led.cleared)
	}
	if len(led.pending) != 0 {
		t.Fatalf("pending after reap = %+v; want empty", led.pending)
	}
	if len(led.seeded) != 0 {
		t.Fatalf("seeded = %v; COLD must not retry", led.seeded)
	}
	for _, line := range up.all() {
		if strings.Contains(line, "UNDELIVERED HANDOVER") {
			t.Fatalf("COLD pending was surfaced: %q", line)
		}
	}
}

func TestT542SweepHandoversMissingAfterClearIsNotListed(t *testing.T) {
	store := handover.NewStore(t.TempDir() + "/handover")
	rec := handover.Pending{
		Agent: "jv-t542-cleared", From: "codex", To: "claude",
		CreatedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}
	if err := store.Put(rec); err != nil {
		t.Fatal(err)
	}
	led := &t542Ledger{store: store}
	if err := led.ClearHandover(rec.Agent); err != nil {
		t.Fatal(err)
	}
	listed, err := led.PendingHandovers()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("List after Clear = %+v; want empty", listed)
	}

	up := &upward{}
	s := &Server{migrator: led}
	s.SetOverseerDeliver(up.deliver)
	s.SweepHandovers()
	again, err := led.PendingHandovers()
	if err != nil || len(again) != 0 {
		t.Fatalf("sweep after Clear listed %+v err=%v", again, err)
	}
	for _, line := range up.all() {
		if strings.Contains(line, "UNDELIVERED HANDOVER") {
			t.Fatalf("cleared file was surfaced after restart sweep: %q", line)
		}
	}
}

// Residual of T542: a live migrate with a real transcript still seeds.
// Empty and missing arms reap/omit; this one must not.
func TestT542SweepHandoversLiveTranscriptStillSeeds(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t542-live", SessionID: "s1", Provider: claudia.ProviderCodex,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	led := &t542Ledger{pending: []handover.Pending{{
		Agent: "jv-t542-live", From: "codex", To: "claude",
		TranscriptPath: "/real.jsonl",
		Kind:           handover.KindMigrate,
		CreatedAt:      time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
	}}}
	up := &upward{}
	s := &Server{registry: reg, migrator: led}
	s.SetOverseerDeliver(up.deliver)
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
		return &recordingSender{}, true, nil
	})
	s.SweepHandovers()
	if len(led.seeded) != 1 || led.seeded[0] != "jv-t542-live" {
		t.Fatalf("seeded = %v; live usable transcript must retry", led.seeded)
	}
	if len(led.cleared) != 0 {
		t.Fatalf("cleared = %v; live usable must not reap", led.cleared)
	}
	for _, line := range up.all() {
		if strings.Contains(line, "UNDELIVERED HANDOVER") {
			t.Fatalf("live usable was surfaced: %q", line)
		}
	}
}

func TestSweepDefaultGrokDoesNotProhibitClaudeMigration(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t542-pin", SessionID: "s1", Provider: claudia.ProviderCodex,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	led := &t542Ledger{}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetMigrator(led)
	s.SetDefaultProvider(string(claudia.ProviderGrok))
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("codex", 0, 100, now),
			t39015Weekly("claude", 80, 20, now),
		}}
	})

	acts := s.SweepPlanPolicy()
	if led.prepared != 1 {
		t.Fatalf("prepared=%d; default Grok is a preference, not a Claude prohibition", led.prepared)
	}
	if len(acts) != 1 || acts[0].Name != "jv-t542-pin" || acts[0].To != "claude" {
		t.Fatalf("want migration to eligible Claude, got %+v", acts)
	}
}

// A standing owner no-Claude pin (ExcludeProviders) is not overwritten by
// plan-usage migrate even when Claude is the only greener dest than the
// exhausted current provider.
func TestT542SweepPlanPolicyDoesNotOverwriteOwnerNoClaudePin(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t542-owner-pin", SessionID: "s1", Provider: claudia.ProviderCodex,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := claudia.OpenSeatPolicyStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := plans.Put("jv-t542-owner-pin", claudia.SeatPolicy{
		ExcludeProviders: []claudia.Provider{claudia.ProviderClaude},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	led := &t542Ledger{}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetSeatPlan(plans)
	s.SetMigrator(led)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("codex", 0, 100, now),
			t39015Weekly("claude", 80, 20, now),
			t39015Weekly("grok", 55, 45, now),
		}}
	})

	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].Name != "jv-t542-owner-pin" {
		t.Fatalf("want one action for the pinned seat, got %+v", acts)
	}
	if acts[0].To == "claude" {
		t.Fatalf("owner no-Claude pin overwritten: %+v", acts[0])
	}
	if acts[0].To != "grok" {
		t.Fatalf("want migrate to grok (Claude excluded), got %+v", acts[0])
	}
	if led.prepared != 1 {
		t.Fatalf("prepared=%d; pin must still migrate to the allowed dest", led.prepared)
	}
}

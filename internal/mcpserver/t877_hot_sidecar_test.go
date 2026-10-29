// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// 🎯T877: a hot Grok seat stored as xai-oauth must vacate through Claudia
// to an eligible destination even when the daemon default provider is Grok.
func TestT877SweepMigratesHotGrokSidecarDespiteDefaultProvider(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-grok-worker", SessionID: "s-grok", Provider: "xai-oauth",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	week := planusage.DefaultWeeklyWindowSeconds
	reading := func(provider string, used, remaining float64, reset time.Time) planusage.Backend {
		return planusage.Backend{Provider: provider, Status: planusage.StatusAvailable, FetchedAt: now,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used,
				RemainingPercent: &remaining, ResetsAt: &reset, LimitWindowSeconds: &week}}}
	}
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			reading("grok", 87, 13, now.Add(7*24*time.Hour/2)),
			reading("codex", 30, 70, now.Add(7*24*time.Hour-time.Hour)),
			reading("claude", 93, 7, now.Add(4*time.Hour)),
		}}
	})
	sw := &fakeSwitcher{reg: reg, live: true}
	s.SetModelSwitcher(sw)
	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].From != "grok" || acts[0].To != "claude" ||
		acts[0].Author != claudia.DecisionAuthor || acts[0].Execution != "switched" {
		t.Fatalf("hot xai-oauth seat must migrate to Claude: %+v", acts)
	}
	if def := reg.Def("jv-grok-worker"); seatprovider.Resolve(def.Provider).Plan != seatprovider.Claude {
		t.Fatalf("registry row = %+v, want Claude launch identity", def)
	}
	if again := s.SweepPlanPolicy(); len(again) != 0 {
		t.Fatalf("second sweep must not move an arrived seat: %+v", again)
	}
	if switches, _ := sw.calls(); len(switches) != 1 {
		t.Fatalf("want exactly one switch, got %v", switches)
	}
}

// Owner exclude_providers is distinct from the daemon default: excluding
// Codex must not be confused with preferring Grok as the fleet default.
func TestT877OwnerCodexBanStillMigratesToClaude(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-grok-worker", SessionID: "s-grok", Provider: "xai-oauth",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := claudia.OpenSeatPolicyStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := plans.Put("jv-grok-worker", claudia.SeatPolicy{
		ExcludeProviders: []claudia.Provider{claudia.ProviderCodex},
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetSeatPlan(plans)
	s.SetDefaultProvider(string(claudia.ProviderGrok))
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	week := planusage.DefaultWeeklyWindowSeconds
	reading := func(provider string, used, remaining float64, reset time.Time) planusage.Backend {
		return planusage.Backend{Provider: provider, Status: planusage.StatusAvailable, FetchedAt: now,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used,
				RemainingPercent: &remaining, ResetsAt: &reset, LimitWindowSeconds: &week}}}
	}
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			reading("grok", 87, 13, now.Add(7*24*time.Hour/2)),
			reading("codex", 30, 70, now.Add(7*24*time.Hour-time.Hour)),
			reading("claude", 93, 7, now.Add(4*time.Hour)),
		}}
	})
	decisions := s.PlanPolicyDecisions()
	if len(decisions) != 1 || decisions[0].Action != planusage.SeatMigrate ||
		decisions[0].To != "claude" || decisions[0].Author != claudia.DecisionAuthor {
		t.Fatalf("Codex ban must still allow Claude despite default Grok: %+v", decisions)
	}
}

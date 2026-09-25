// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func t39015Weekly(name string, rem, used float64, now time.Time) planusage.Backend {
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	return planusage.Backend{
		Provider: name, Status: planusage.StatusAvailable,
		Windows: []planusage.Window{{
			Name: planusage.WindowWeekly, RemainingPercent: &rem, UsedPercent: &used,
			ResetsAt: &week, LimitWindowSeconds: &lim,
		}},
	}
}

func TestStitchOmitProviderUsesPlanDestWhenDefaultAhead(t *testing.T) {
	t791Steerable(t) // 🎯T791: subject is not steerability
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	// 🎯T583: Claude is deliberately absent from this feed. When Claude is
	// published with headroom the claude-first knob decides and plan_dest
	// never runs; the usage-first question this test asks only arises
	// among the other backends.
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("grok", 45, 55, now),
			t39015Weekly("codex", 80, 20, now),
		}}
	})
	def, _, note, err := s.stitchAgentStart(
		"jv-t39015-dest", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.Provider("openai-codex") {
		t.Fatalf("omit mint dest=%q want openai-codex (grok ahead)", def.Provider)
	}
	if !strings.Contains(note, "provider_knob: claudia") {
		t.Fatalf("note should cite claudia: %q", note)
	}
}

func TestStitchOmitProviderRefusesWhenDestEmpty(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("grok", 20, 80, now),
		}}
	})
	_, _, _, err = s.stitchAgentStart(
		"jv-t39015-refuse", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err == nil || !strings.Contains(err.Error(), "plan dest empty") {
		t.Fatalf("want dest-empty refuse, err=%v", err)
	}
}

func TestColdSwitchStaysAndLaunches(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "claudia-po", SessionID: "s-po", Provider: claudia.ProviderClaude,
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("claude", 10, 90, now),
			t39015Weekly("cursor", 80, 20, now),
		}}
	})
	led := &sweepLedger{cold: true}
	s.SetMigrator(led)
	s.SweepPlanPolicy()
	if len(led.launched) != 1 || led.launched[0] != "claudia-po" {
		t.Fatalf("launched=%v; cold switch must launch", led.launched)
	}
	if rec := store.Snapshot().Agents["claudia-po"]; rec.State == fleetintent.Parked {
		t.Fatalf("cold switch parked the seat: %+v", rec)
	}
}

func TestColdSwitchParkLiftsWhenProviderIsCool(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "claudia-po", SessionID: "s-po", Provider: claudia.ProviderCursor,
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-deliberate", SessionID: "s-d", Provider: claudia.ProviderCursor,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	s.MarkAgentParked("claudia-po", "jevons", "weekly hot or exhausted: prepare returned COLD (no predecessor transcript)")
	s.MarkAgentParked("jv-deliberate", "jevons", "owner asked this seat to stop")
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("claude", 10, 90, now),
			t39015Weekly("cursor", 80, 20, now),
		}}
	})
	led := &sweepLedger{}
	s.SetMigrator(led)
	s.SweepPlanPolicy()
	if len(led.launched) != 1 || led.launched[0] != "claudia-po" {
		t.Fatalf("launched=%v; want only the cold-switch park lifted", led.launched)
	}
	if rec := store.Snapshot().Agents["claudia-po"]; rec.State != fleetintent.Working {
		t.Fatalf("claudia-po intent=%q", rec.State)
	}
	if rec := store.Snapshot().Agents["jv-deliberate"]; rec.State != fleetintent.Parked {
		t.Fatalf("deliberate park lifted: %+v", rec)
	}
}

func TestSweepParksWhenDestEmpty(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	zero, used := 0.0, 100.0
	week := now.Add(3 * 24 * time.Hour)
	lim := planusage.DefaultWeeklyWindowSeconds
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{{
			Provider: "grok", Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{
				Name: planusage.WindowWeekly, RemainingPercent: &zero, UsedPercent: &used,
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}}}
	})
	if _, err := s.registry.EnsureAgentWithParent("w1", t.TempDir(), "", "jevons-po", true); err != nil {
		t.Fatal(err)
	}
	def := s.registry.Def("w1")
	def.Provider = claudia.ProviderGrok
	def.Purpose = claudia.PurposeWork
	if err := s.registry.Register(*def); err != nil {
		t.Fatal(err)
	}
	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].Name != "w1" || acts[0].To != "" {
		t.Fatalf("want park w1, got %+v", acts)
	}
	if got := s.fleetIntent().AgentState("w1"); string(got) != string(fleetintent.Parked) {
		t.Fatalf("intent=%q want parked", got)
	}
}

func TestT850SweepMovesHotPOAndKeepsItsHandover(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("claude", 20, 80, now),
			t39015Weekly("codex", 80, 20, now),
			t39015Weekly("grok", 55, 45, now),
		}}
	})
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", SessionID: "s-root", Purpose: claudia.PurposeOverseer, Provider: claudia.ProviderGrok},
		{Name: "jevons-po", SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Provider: claudia.ProviderClaude},
		{Name: "jv-t517-worker", SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po", Provider: claudia.ProviderClaude},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	led := &sweepLedger{pending: []handover.Pending{
		{Agent: "jevons-po", TranscriptPath: "/po.jsonl"},
		{Agent: "jv-t517-worker", TranscriptPath: "/w.jsonl"},
	}}
	s.migrator = led
	acts := s.SweepPlanPolicy()
	got := map[string]string{}
	for _, a := range acts {
		got[a.Name] = a.To
	}
	if got["jevons-po"] != "codex" || got["jv-t517-worker"] != "codex" || len(acts) != 2 {
		t.Fatalf("hot PO and worker move to codex: %+v", acts)
	}
	if _, present := got["jevons"]; present {
		t.Fatalf("overseer on grok must stay out: %+v", acts)
	}
	for _, name := range led.cleared {
		if name == "jevons-po" {
			t.Fatalf("hot PO handover must stay, cleared=%v", led.cleared)
		}
	}
}

// A second sweep while the registry still names the hot provider does
// not call PrepareMigration again: the handover written by the first
// sweep is the guard. Once the PO's provider is the dest, MigrateOff is
// false, so the sweep emits no action and does not reap the handover.
func TestT850SecondSweepDoesNotPrepareAgainWhileProviderIsHot(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("claude", 20, 80, now),
			t39015Weekly("codex", 80, 20, now),
			t39015Weekly("grok", 55, 45, now),
		}}
	})
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", SessionID: "s-root", Purpose: claudia.PurposeOverseer, Provider: claudia.ProviderGrok},
		{Name: "jevons-po", SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Provider: claudia.ProviderClaude},
		{Name: "jv-worker", SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po", Provider: claudia.ProviderClaude},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	led := &sweepLedger{}
	s.migrator = led
	s.SweepPlanPolicy()
	s.SweepPlanPolicy()
	if led.prepared != 2 {
		t.Fatalf("prepared=%d; want one prepare each for PO and worker, not a second pass", led.prepared)
	}
	def := reg.Def("jevons-po")
	if def == nil {
		t.Fatal("missing jevons-po")
	}
	def.Provider = claudia.ProviderCodex
	if err := reg.Register(*def); err != nil {
		t.Fatal(err)
	}
	acts := s.SweepPlanPolicy()
	for _, a := range acts {
		if a.Name == "jevons-po" {
			t.Fatalf("arrived PO must not be acted on: %+v", acts)
		}
		if a.To == "claude" {
			t.Fatalf("must not send anyone back onto claude: %+v", acts)
		}
	}
	if led.prepared != 2 {
		t.Fatalf("prepared=%d after arrival; the third sweep must not prepare again", led.prepared)
	}
	for _, name := range led.cleared {
		if name == "jevons-po" {
			t.Fatalf("arrived PO handover must not be reaped for being a PO, cleared=%v", led.cleared)
		}
	}
}

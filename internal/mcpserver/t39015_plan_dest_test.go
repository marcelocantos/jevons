// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/fleetintent"
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

func TestT691PlanPolicyForwardsPersistedSeatProviderConstraints(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		prefer  claudia.Provider
		allowed []claudia.Provider
		exclude []claudia.Provider
		want    string
	}{
		{name: "preference", prefer: claudia.ProviderCodex, want: "codex"},
		{name: "explicit exclusion beats preference", prefer: claudia.ProviderCodex,
			exclude: []claudia.Provider{claudia.ProviderCodex}, want: "grok"},
		{name: "allowed set beats preference", prefer: claudia.ProviderCodex,
			allowed: []claudia.Provider{claudia.ProviderClaude, claudia.ProviderGrok}, want: "grok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "agents.json")
			planPath := dir
			reg, err := claudia.NewRegistry(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := reg.Register(claudia.AgentDef{
				Name: "worker", SessionID: "session-worker", Provider: claudia.ProviderClaude,
				Purpose: claudia.PurposeWork,
			}); err != nil {
				t.Fatal(err)
			}
			plans, err := claudia.OpenSeatPolicyStore(planPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := plans.Put("worker", claudia.SeatPolicy{
				PreferProvider: tc.prefer, AllowedProviders: tc.allowed,
				AllowNone: tc.allowed != nil && len(tc.allowed) == 0, ExcludeProviders: tc.exclude,
			}); err != nil {
				t.Fatal(err)
			}
			reloaded, err := claudia.NewRegistry(path)
			if err != nil {
				t.Fatal(err)
			}
			reloadedPlans, err := claudia.OpenSeatPolicyStore(planPath)
			if err != nil {
				t.Fatal(err)
			}
			s := New(t.TempDir(), nil, nil)
			s.SetRegistry(reloaded)
			s.SetSeatPlan(reloadedPlans)
			s.SetPlanUsageSource(func() planusage.Snapshot {
				return planusage.Snapshot{At: now, Backends: []planusage.Backend{
					t39015Weekly("claude", 20, 80, now),
					t39015Weekly("codex", 80, 20, now),
					t39015Weekly("grok", 55, 45, now),
				}}
			})
			decisions := s.PlanPolicyDecisions()
			if len(decisions) != 1 || decisions[0].Action != planusage.SeatMigrate ||
				decisions[0].To != tc.want || decisions[0].Author != claudia.DecisionAuthor {
				t.Fatalf("Claudia placement with persisted seat policy = %+v; want %s", decisions, tc.want)
			}
		})
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
	// Claude is absent from this feed so the usage-first question this
	// test asks (grok ahead vs codex ok) is not entangled with anything
	// Claude-specific — there is no such knob any more (🎯T1013.6).
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
	if cli.PlanProvider(def.Provider) != claudia.ProviderCodex {
		t.Fatalf("omit mint dest=%q want codex (grok ahead)", def.Provider)
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

func TestSweepDefersWhenDestinationFeedIsIncomplete(t *testing.T) {
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
	if len(acts) != 0 {
		t.Fatalf("incomplete feed must defer rather than park w1: %+v", acts)
	}
	decisions := s.PlanPolicyDecisions()
	if len(decisions) != 1 || string(decisions[0].Action) != "defer" || decisions[0].Reason == "" {
		t.Fatalf("decision surface must explain the defer: %+v", decisions)
	}
	if got := s.fleetIntent().AgentState("w1"); got == fleetintent.Parked {
		t.Fatalf("incomplete feed parked w1: %q", got)
	}
}

func TestSweepDoesNotParkWhenSwitcherIsUnavailable(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "claudia-po", SessionID: "source", Provider: claudia.ProviderClaude,
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
	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].To != "cursor" {
		t.Fatalf("fixture must request a move: %+v", acts)
	}
	if acts[0].Execution != "deferred" || acts[0].Failure != "model switcher not configured" {
		t.Fatalf("an unavailable switch must be reported, not implied complete: %+v", acts[0])
	}
	decisions := s.PlanPolicyDecisions()
	if len(decisions) != 1 || decisions[0].Execution != "deferred" ||
		decisions[0].Failure != "model switcher not configured" {
		t.Fatalf("read-only decision lost the host failure: %+v", decisions)
	}
	if got := store.Snapshot().Agents["claudia-po"].State; got == fleetintent.Parked {
		t.Fatalf("a missing switcher parked a seat with a valid destination: %q", got)
	}
}

func TestSweepReportsFailedLaunchAfterSwitchAsPending(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "claudia-po", SessionID: "source", Provider: claudia.ProviderClaude,
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("claude", 10, 90, now),
			t39015Weekly("cursor", 80, 20, now),
		}}
	})
	s.SetModelSwitcher(&fakeSwitcher{reg: reg, launchErr: errors.New("destination unavailable")})
	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].Execution != "pending" ||
		acts[0].Failure != "destination unavailable" {
		t.Fatalf("failed launch must be visible as pending, not switched: %+v", acts)
	}
	// The switch itself landed: the seat's row is on its destination, so the
	// next read of the plan sees nothing to move.
	if def := reg.Def("claudia-po"); def.Provider != "cursor" || def.SessionID != "source" {
		t.Fatalf("def = %+v, want cursor on session source", def)
	}
	if decisions := s.PlanPolicyDecisions(); len(decisions) != 1 || decisions[0].Action == planusage.SeatMigrate {
		t.Fatalf("a landed switch was decided again: %+v", decisions)
	}
}

func TestT850SweepMovesHotPO(t *testing.T) {
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
	s.SetModelSwitcher(&fakeSwitcher{reg: reg, live: true})
	acts := s.SweepPlanPolicy()
	got := map[string]string{}
	for _, a := range acts {
		got[a.Name] = a.To
		if a.Execution != "switched" {
			t.Fatalf("%s was not switched: %+v", a.Name, a)
		}
	}
	if got["jevons-po"] != "codex" || got["jv-t517-worker"] != "codex" || len(acts) != 2 {
		t.Fatalf("hot PO and worker move to codex: %+v", acts)
	}
	if _, present := got["jevons"]; present {
		t.Fatalf("overseer on grok must stay out: %+v", acts)
	}
}

// A switch rewrites the row at once, so the next sweep reads the seat on
// its destination and does not move it again.
func TestT850SecondSweepDoesNotSwitchAgain(t *testing.T) {
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
	sw := &fakeSwitcher{reg: reg, live: true}
	s.SetModelSwitcher(sw)
	s.SweepPlanPolicy()
	acts := s.SweepPlanPolicy()
	if switches, _ := sw.calls(); len(switches) != 2 {
		t.Fatalf("switches = %v; want one each for PO and worker, not a second pass", switches)
	}
	for _, a := range acts {
		if a.To == "claude" || a.Execution == "switched" {
			t.Fatalf("second sweep acted on an arrived seat: %+v", acts)
		}
	}
}

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
			t39015Weekly("grok", 45, 55, now),
			t39015Weekly("claude", 80, 20, now),
		}}
	})
	def, _, note, err := s.stitchAgentStart(
		"jv-t39015-dest", t.TempDir(), "", "", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider != claudia.ProviderClaude {
		t.Fatalf("omit mint dest=%q want claude (grok ahead)", def.Provider)
	}
	if !strings.Contains(note, "plan_dest") {
		t.Fatalf("note should cite plan_dest: %q", note)
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

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T977: when a plan comes back, the overseer and every running product
// owner are told to resume; workers are left to their PO; a plan that stays
// hot, or stays ok, says nothing.
func TestT977CapacityRestoredWakesOverseerAndPOs(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(context.Context, claudia.Config) (*claudia.Agent, error) {
		return claudia.NewStubAgent(nil), nil
	}})
	for _, name := range []string{"jevons-po", "claudia-po", "jv-t977-worker", "stopped-po"} {
		if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: dir, Provider: "anthropic", SessionID: name}); err != nil {
			t.Fatal(err)
		}
		if name != "stopped-po" {
			if _, err := reg.Launch(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	s := &Server{}
	s.registry = reg
	s.observeRegistryLiveness()
	for _, name := range []string{"jevons-po", "claudia-po", "jv-t977-worker"} {
		d := reg.Def(name)
		s.Seats().FromClaudia(seatstate.SeatReport{Name: name, SessionID: d.SessionID, Provider: string(d.Provider), Alive: true, Known: true}, time.Now())
	}
	type sent struct{ to, text string }
	var got []sent
	s.capacityDeliver = func(name, text string) error {
		got = append(got, sent{name, text})
		return nil
	}
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	used := 92.0
	s.SetPlanUsageSource(func() planusage.Snapshot {
		rem := 100 - used
		resets := now.Add(84 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{{
			Provider: "claude", Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}},
		}}}
	})

	if told := s.NoteCapacity(); len(told) != 0 {
		t.Fatalf("a hot plan told %v", told)
	}
	if told := s.NoteCapacity(); len(told) != 0 {
		t.Fatalf("a plan still hot told %v", told)
	}
	used = 10 // the window reset
	told := s.NoteCapacity()
	slices.Sort(told)
	if want := []string{"claudia-po", "jevons", "jevons-po"}; !slices.Equal(told, want) {
		t.Fatalf("told %v, want %v (overseer and running POs, no worker, no stopped PO)", told, want)
	}
	if !strings.Contains(got[0].text, "claude") || !strings.Contains(got[0].text, "resume") {
		t.Fatalf("notice does not name the plan or say resume: %q", got[0].text)
	}
	if told := s.NoteCapacity(); len(told) != 0 {
		t.Fatalf("a plan that stayed ok told again: %v", told)
	}
}

// 🎯T977 (2026-10-06 recurrence): a fleet-wide Parked intent recorded for
// capacity by a product path is lifted automatically on restoration, so the
// hold that otherwise defers every per-agent resume (planIntentDeferral
// checks fleet-wide intent first) does not outlive the capacity it was
// parked for.
func TestT977CapacityRestoredLiftsCapacityParkedFleetIntent(t *testing.T) {
	dir := t.TempDir()
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetFleetIntentStore(store)
	if err := s.SetFleetIntent(fleetintent.Parked, "product:plan_policy",
		"claude weekly exhausted (98% used); standing the fleet down for capacity"); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	used := 92.0
	s.SetPlanUsageSource(func() planusage.Snapshot {
		rem := 100 - used
		resets := now.Add(84 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{{
			Provider: "claude", Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}},
		}}}
	})
	s.capacityDeliver = func(string, string) error { return nil }

	s.NoteCapacity() // first reading only arms the watch; fleet intent stays held.
	if got := s.fleetIntent().FleetState(); got != fleetintent.Parked {
		t.Fatalf("fleet state after first reading = %v, want still parked", got)
	}

	used = 10 // the window reset
	s.NoteCapacity()
	if got := s.fleetIntent().FleetState(); got != fleetintent.Working {
		t.Fatalf("fleet state after restoration = %v, want working", got)
	}
	if by := s.fleetIntent().Fleet.By; by != capacityRestoreActor {
		t.Fatalf("fleet intent by = %q, want %q", by, capacityRestoreActor)
	}
}

// An owner-recorded Parked intent is never lifted by this evidence (🎯T969),
// even when its reason reads as capacity-related.
func TestT977CapacityRestoredDoesNotLiftOwnerParkedFleetIntent(t *testing.T) {
	dir := t.TempDir()
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetFleetIntentStore(store)
	if err := s.SetFleetIntent(fleetintent.Parked, "owner",
		"standing the fleet down for capacity while I'm travelling"); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	used := 92.0
	s.SetPlanUsageSource(func() planusage.Snapshot {
		rem := 100 - used
		resets := now.Add(84 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{{
			Provider: "claude", Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}},
		}}}
	})
	s.capacityDeliver = func(string, string) error { return nil }

	s.NoteCapacity()
	used = 10
	s.NoteCapacity()
	if got := s.fleetIntent().FleetState(); got != fleetintent.Parked {
		t.Fatalf("fleet state after restoration = %v, want still parked (owner-recorded, 🎯T969)", got)
	}
}

// A fleet-wide BlockedProvider intent is left to its own, more conservative
// clearance (🎯T406: a successful provider call) — a plan restoration alone
// does not settle whether the provider itself is still refusing (revoked
// key, spend wall).
func TestT977CapacityRestoredDoesNotLiftBlockedProviderFleetIntent(t *testing.T) {
	dir := t.TempDir()
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetFleetIntentStore(store)
	if err := s.SetFleetIntent(fleetintent.BlockedProvider, hardBlockBy,
		"provider hard-block: revoked key"); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	used := 92.0
	s.SetPlanUsageSource(func() planusage.Snapshot {
		rem := 100 - used
		resets := now.Add(84 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{{
			Provider: "claude", Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}},
		}}}
	})
	s.capacityDeliver = func(string, string) error { return nil }

	s.NoteCapacity()
	used = 10
	s.NoteCapacity()
	if got := s.fleetIntent().FleetState(); got != fleetintent.BlockedProvider {
		t.Fatalf("fleet state after restoration = %v, want still blocked_provider (🎯T406 owns this clearance)", got)
	}
}

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
	"github.com/marcelocantos/jevons/internal/planusage"
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

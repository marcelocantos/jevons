// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

const t1196MaxLiveLedger = `
schema_version: 1
project: test
targets:
  T11961:
    name: ready leaf A for max-live sweep test
    status: identified
    acceptance:
    - ship
  T11962:
    name: ready leaf B for max-live sweep test
    status: identified
    acceptance:
    - ship
`

// 🎯T1196 / 🎯T419: drive frontierConsumeSweep (production assembly path,
// Spawn nil → real spawnFrontierWorker) with the live-worker ceiling at
// capacity, then again after one sweep worker is reaped.
func TestT1196MaxLiveWorkersSweepFunction(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t1196MaxLiveLedger), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}

	liveWorkers := []string{"jv-t9001-auto", "jv-t9002-auto", "jv-t9003-auto", "jv-t9004-auto"}
	for i, name := range liveWorkers {
		if err := reg.Register(claudia.AgentDef{
			Name: name, WorkDir: dir, SessionID: "s-" + name,
			Purpose: claudia.PurposeWork, Parent: "jevons-po",
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.SetAgent(name, fleetintent.Working, "auto:frontier", "", time.Now().Add(-time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)
	s.SetTurnWitness(witnessYielding(TurnEvidence{ConversationGrew: true, Detail: "test: stub brief delivery"}))
	t.Cleanup(reg.StopAll)

	loop := FrontierConsumeLoopArgs{
		Server:         s,
		Workdir:        dir,
		ParentPO:       "jevons-po",
		MaxLiveWorkers: 4,
		// Spawn nil: production spawnFrontierWorker runs (🎯T419 call site).
	}

	reps := s.frontierConsumeSweep(loop, nil)
	spawned := 0
	for _, r := range reps {
		if r.Action == FrontierConsumeSpawn {
			spawned++
		}
		if r.TargetID == "T11961" || r.TargetID == "T11962" {
			if r.Action != FrontierConsumeSkip || r.Reason != FrontierReasonMaxLiveWorkers {
				t.Fatalf("%s at cap: %+v, want skip %s", r.TargetID, r, FrontierReasonMaxLiveWorkers)
			}
		}
	}
	if spawned != 0 {
		t.Fatalf("first sweep spawned %d, want 0 at live cap", spawned)
	}

	if err := store.SetAgent(liveWorkers[0], fleetintent.Reaped, "product:kill", "", time.Now()); err != nil {
		t.Fatal(err)
	}

	reps = s.frontierConsumeSweep(loop, nil)
	spawned = 0
	for _, r := range reps {
		if r.Action == FrontierConsumeSpawn {
			spawned++
		}
	}
	if spawned != 1 {
		t.Fatalf("second sweep spawned %d, want 1 after one worker reaped", spawned)
	}
}

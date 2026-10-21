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

const t1109SweepFunctionLedger = `
schema_version: 1
project: test
targets:
  T1200:
    name: a ready frontier leaf for the sweep-function test
    status: identified
    acceptance:
    - "ship hermetic fix"
`

// 🎯T1109 acceptance 2: drive the frontier sweep's OWN sweep function
// (frontierConsumeSweep, which runs the real ledger load + leaf
// classification + engagement observation, then — with no Spawn override —
// calls its production default of s.spawnFrontierWorker) against a registry
// row that is the exact shape 🎯T560's kill-with-descendants-preserved
// leaves behind: still registered, stopped, and stamped intent Reaped. This
// is deliberately NOT a hand-rolled substitute for the sweep: unlike every
// other frontier_consume test in this package, args.Spawn is left nil, so
// the sweep's real default spawn path runs, formatting the real auto-spawn
// brief and calling the real spawnFrontierWorker — the function
// TestT1109SweepSpawnPathResetsReapedIntent already pins directly.
func TestT1109SweepFunctionResetsReapedIntent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t1109SweepFunctionLedger), 0o644); err != nil {
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

	const name = "jv-t1200-auto" // FrontierWorkerName("T1200")
	// The row 🎯T560's kill-with-descendants-preserved leaves behind: still
	// registered, stopped, and stamped Reaped — exactly what a sweep
	// re-mint of a once-killed name finds.
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-" + name,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAgent(name, fleetintent.Reaped, "product:kill", "", time.Now().Add(-2*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetFleetIntentStore(store)
	// Short-circuit turn-confirmation: the stub launcher never produces
	// real turn evidence, and the sweep-level spawn path (unlike the
	// direct spawnFrontierWorker call in the companion test) formats and
	// delivers a real non-empty auto-spawn brief, which without this would
	// block on the production confirm window — and, worse, 🎯T956's
	// once-only deadline extension to the hard 5-minute bound once the
	// stub process reports alive, which a shortened JEVONS_TURN_CONFIRM_WINDOW
	// does not touch. This is the documented test seam (SetTurnWitness),
	// not a change to production confirmation behaviour.
	s.SetTurnWitness(witnessYielding(TurnEvidence{ConversationGrew: true, Detail: "test: stub brief delivery"}))
	t.Cleanup(reg.StopAll)

	if got := s.fleetIntent().AgentState(name); got != fleetintent.Reaped {
		t.Fatalf("precondition: intent = %q, want reaped", got)
	}

	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:            s,
		Workdir:           dir,
		ParentPO:          "jevons-po",
		MaxSpawnsPerCycle: 5,
		// Spawn deliberately left nil: the sweep's own production default
		// (s.spawnFrontierWorker) is what must reset intent, not a test
		// substitute.
	}, nil)

	var r1200 *FrontierConsumeReport
	for i := range reps {
		if reps[i].TargetID == "T1200" {
			r1200 = &reps[i]
		}
	}
	if r1200 == nil {
		t.Fatalf("no report for T1200: %+v", reps)
	}
	if r1200.Action != FrontierConsumeSpawn || r1200.Worker != name {
		t.Fatalf("T1200: %+v, want spawn of %q", *r1200, name)
	}

	if got := s.fleetIntent().AgentState(name); got != fleetintent.Working {
		t.Fatalf("intent after sweep spawn = %q, want working (the sweep's own spawn path must reset a previously-reaped name's intent, 🎯T1109)", got)
	}

	// Companion to the acceptance's second clause: once intent reads
	// Working again, the 🎯T984 stand-down sweep must leave the seat alone.
	found := s.stopSeatsAgainstIntent(nil)
	for _, v := range found {
		if v.Name == name {
			t.Fatalf("stand-down sweep stopped a seat the frontier sweep just started with intent working: %+v", v)
		}
	}
}

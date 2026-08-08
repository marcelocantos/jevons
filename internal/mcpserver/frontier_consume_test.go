// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T254.1 acceptance: file a ready leaf → worker auto-spawned under the
// product PO within one sweep, or an explicit park/skip reason — no owner
// spawn command in the loop.

var errFake = errors.New("fake spawn failure")

func frontierNow() time.Time {
	return time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
}

func TestSweepFrontierConsumeSpawnsReadyLeaf(t *testing.T) {
	var gotLeaf, gotWorker string
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{ID: "T500", Name: "New unengaged Build leaf"},
		},
		Now:          frontierNow(),
		PORegistered: true,
		Spawn: func(leaf poproactive.LeafObs, workerName string) error {
			gotLeaf, gotWorker = leaf.ID, workerName
			return nil
		},
	})
	if len(reps) != 1 {
		t.Fatalf("reports = %+v", reps)
	}
	r := reps[0]
	if r.Action != FrontierConsumeSpawn || r.Reason != FrontierReasonSpawned {
		t.Fatalf("action=%s reason=%s, want spawn/spawned", r.Action, r.Reason)
	}
	if gotLeaf != "T500" || gotWorker != "jv-t500-auto" || r.Worker != gotWorker {
		t.Fatalf("spawned leaf=%q worker=%q rep=%q", gotLeaf, gotWorker, r.Worker)
	}
}

func TestSweepFrontierConsumeSkipsGatedEngagedBlocked(t *testing.T) {
	spawned := 0
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{ID: "T1", Name: "Design-gated leaf", Tags: []string{"design-gated"}},
			{ID: "T2", Name: "Owner parked", Context: "File only, do not implement until Build opened."},
			{ID: "T3", Name: "Engaged leaf", AlreadyEngaged: true},
			{ID: "T4", Name: "Blocked leaf", Blocked: true},
			{ID: "T5", Name: "Closed leaf", Closed: true},
		},
		Now:          frontierNow(),
		PORegistered: true,
		Spawn: func(poproactive.LeafObs, string) error {
			spawned++
			return nil
		},
	})
	if spawned != 0 {
		t.Fatalf("spawned %d workers from gated/engaged/blocked leaves", spawned)
	}
	wantReasons := map[string]string{
		"T1": FrontierReasonDesignGated,
		"T2": FrontierReasonDesignGated,
		"T3": FrontierReasonEngaged,
		"T4": FrontierReasonBlocked,
		"T5": FrontierReasonClosed,
	}
	for _, r := range reps {
		if r.Action != FrontierConsumeSkip {
			t.Errorf("%s: action=%s, want skip", r.TargetID, r.Action)
		}
		if r.Reason != wantReasons[r.TargetID] {
			t.Errorf("%s: reason=%s, want %s", r.TargetID, r.Reason, wantReasons[r.TargetID])
		}
	}
}

func TestSweepFrontierConsumeCycleCapParksExplicitly(t *testing.T) {
	spawned := 0
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{ID: "T1", Name: "Ready A"},
			{ID: "T2", Name: "Ready B"},
			{ID: "T3", Name: "Ready C"},
		},
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 1,
		Spawn: func(poproactive.LeafObs, string) error {
			spawned++
			return nil
		},
	})
	if spawned != 1 {
		t.Fatalf("spawned = %d, want 1 (cycle cap)", spawned)
	}
	if reps[0].Action != FrontierConsumeSpawn {
		t.Fatalf("first leaf action = %s", reps[0].Action)
	}
	for _, r := range reps[1:] {
		if r.Action != FrontierConsumePark || r.Reason != FrontierReasonCapacity {
			t.Errorf("%s: action=%s reason=%s, want park/park_capacity", r.TargetID, r.Action, r.Reason)
		}
	}
}

func TestSweepFrontierConsumePOMissingAndSpawnHaltedPark(t *testing.T) {
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves:       []poproactive.LeafObs{{ID: "T1", Name: "Ready"}},
		Now:          frontierNow(),
		PORegistered: false,
		Spawn: func(poproactive.LeafObs, string) error {
			t.Fatal("must not spawn without registered PO")
			return nil
		},
	})
	if reps[0].Action != FrontierConsumePark || reps[0].Reason != FrontierReasonPOMissing {
		t.Fatalf("PO-missing: %+v", reps[0])
	}

	reps = SweepFrontierConsume(FrontierConsumeArgs{
		Leaves:       []poproactive.LeafObs{{ID: "T1", Name: "Ready"}},
		Now:          frontierNow(),
		PORegistered: true,
		SpawnHalted:  "budget clamp: spawn halted",
		Spawn: func(poproactive.LeafObs, string) error {
			t.Fatal("must not spawn under budget clamp")
			return nil
		},
	})
	if reps[0].Action != FrontierConsumePark || reps[0].Reason != FrontierReasonSpawnHalted {
		t.Fatalf("spawn-halted: %+v", reps[0])
	}
}

func TestSweepFrontierConsumeBackoffAndMaxFromDurableLedger(t *testing.T) {
	ledger, err := OpenFrontierSpawnLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := frontierNow()
	spawnOK := func(poproactive.LeafObs, string) error { return nil }
	leaf := []poproactive.LeafObs{{ID: "T1", Name: "Ready"}}

	// First sweep spawns and records.
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: leaf, Ledger: ledger, Now: now, PORegistered: true, Spawn: spawnOK,
	})
	if reps[0].Action != FrontierConsumeSpawn {
		t.Fatalf("first: %+v", reps[0])
	}
	// Immediately after (worker died / finished-not-achieved): backoff parks.
	reps = SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: leaf, Ledger: ledger, Now: now.Add(time.Minute), PORegistered: true, Spawn: spawnOK,
	})
	if reps[0].Action != FrontierConsumePark || reps[0].Reason != FrontierReasonBackoff {
		t.Fatalf("backoff: %+v", reps[0])
	}
	// After backoff: spawns again until max, then parks with max_autospawns.
	at := now
	for i := 1; i < DefaultFrontierConsumeMaxSpawnsPerTarget; i++ {
		at = at.Add(DefaultFrontierConsumeRespawnBackoff + time.Minute)
		reps = SweepFrontierConsume(FrontierConsumeArgs{
			Leaves: leaf, Ledger: ledger, Now: at, PORegistered: true, Spawn: spawnOK,
		})
		if reps[0].Action != FrontierConsumeSpawn {
			t.Fatalf("respawn %d: %+v", i, reps[0])
		}
	}
	at = at.Add(DefaultFrontierConsumeRespawnBackoff + time.Minute)
	reps = SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: leaf, Ledger: ledger, Now: at, PORegistered: true, Spawn: spawnOK,
	})
	if reps[0].Action != FrontierConsumePark || reps[0].Reason != FrontierReasonMaxAutospawns {
		t.Fatalf("max: %+v", reps[0])
	}

	// Durable across reopen.
	reopened, err := OpenFrontierSpawnLedger(ledgerDirOf(t, ledger))
	if err != nil {
		t.Fatal(err)
	}
	count, _ := reopened.Get("T1")
	if count != DefaultFrontierConsumeMaxSpawnsPerTarget {
		t.Fatalf("reopened count = %d, want %d", count, DefaultFrontierConsumeMaxSpawnsPerTarget)
	}
}

// ledgerDirOf recovers the state dir from a ledger path (…/fleet/frontier_consume.json).
func ledgerDirOf(t *testing.T, l *FrontierSpawnLedger) string {
	t.Helper()
	p := l.path
	const suffix = "/fleet/frontier_consume.json"
	if !strings.HasSuffix(p, suffix) {
		t.Fatalf("unexpected ledger path %q", p)
	}
	return strings.TrimSuffix(p, suffix)
}

func TestSweepFrontierConsumeSpawnFailureParksLoudly(t *testing.T) {
	ledger, err := OpenFrontierSpawnLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves:       []poproactive.LeafObs{{ID: "T1", Name: "Ready"}},
		Ledger:       ledger,
		Now:          frontierNow(),
		PORegistered: true,
		Spawn: func(poproactive.LeafObs, string) error {
			return errFake
		},
	})
	if reps[0].Action != FrontierConsumePark || reps[0].Reason != FrontierReasonSpawnFailed {
		t.Fatalf("spawn-fail: %+v", reps[0])
	}
	// Failed spawn must not consume the target's auto-spawn budget.
	if count, _ := ledger.Get("T1"); count != 0 {
		t.Fatalf("failed spawn recorded in ledger (count=%d)", count)
	}
}

func TestFrontierWorkerNameLiteralDots(t *testing.T) {
	// 🎯T197: hierarchical ids keep literal dots.
	if got := FrontierWorkerName("T254.1"); got != "jv-t254.1-auto" {
		t.Fatalf("worker name = %q", got)
	}
	if got := FrontierWorkerName("🎯T159"); got != "jv-t159-auto" {
		t.Fatalf("flat worker name = %q", got)
	}
}

func TestFormatFrontierSpawnBriefCarriesMission(t *testing.T) {
	brief := FormatFrontierSpawnBrief(targetfile.FrontierLeaf{
		ID:         "T500",
		Name:       "The daemon does the thing",
		Acceptance: []string{"Hermetic: thing done"},
		Context:    "Why the thing matters.",
	}, "jevons-po")
	for _, want := range []string{"🎯T500", "The daemon does the thing", "Hermetic: thing done", "jevons-po", "T104"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q:\n%s", want, brief)
		}
	}
}

// --- assembly: file leaf → worker engaged or park, no owner spawn command ---

const frontierConsumeTestLedger = `
targets:
  T500:
    name: New unengaged Build leaf
    status: identified
    acceptance:
    - "Hermetic: thing done"
  T501:
    name: Already engaged leaf
    status: identified
  T502:
    name: Design-gated leaf
    status: identified
    tags:
    - design-gated
  T503:
    name: Achieved already
    status: achieved
`

func TestFrontierConsumeSweepAssembly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(frontierConsumeTestLedger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Product PO registered (T129 lineage precondition).
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	// T501 already has an engaged implementer.
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t501-existing", WorkDir: dir, SessionID: "s1",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T501",
	}); err != nil {
		t.Fatal(err)
	}

	s := New(dir, nil, nil)
	s.SetRegistry(reg)

	ledger, err := OpenFrontierSpawnLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	loopArgs := FrontierConsumeLoopArgs{
		Server:   s,
		Workdir:  dir,
		ParentPO: "jevons-po",
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			// Simulate the production spawn: register an engaged worker under the PO.
			return reg.Register(claudia.AgentDef{
				Name: workerName, WorkDir: dir, SessionID: "spawned",
				Purpose: claudia.PurposeWork, Parent: parent, TargetID: leaf.ID,
			})
		},
	}

	reps := s.frontierConsumeSweep(loopArgs, ledger)
	byTarget := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byTarget[r.TargetID] = r
	}
	if r := byTarget["T500"]; r.Action != FrontierConsumeSpawn || r.Worker != "jv-t500-auto" {
		t.Fatalf("T500: %+v", r)
	}
	if r := byTarget["T501"]; r.Action != FrontierConsumeSkip || r.Reason != FrontierReasonEngaged {
		t.Fatalf("T501: %+v", r)
	}
	if r := byTarget["T502"]; r.Action != FrontierConsumeSkip || r.Reason != FrontierReasonDesignGated {
		t.Fatalf("T502: %+v", r)
	}
	if _, ok := byTarget["T503"]; ok {
		t.Fatalf("achieved target must not be a frontier leaf: %+v", byTarget["T503"])
	}
	// Spawned worker is registered under the PO with the target bound.
	def := reg.Def("jv-t500-auto")
	if def == nil || def.Parent != "jevons-po" || def.TargetID != "T500" {
		t.Fatalf("spawned worker def = %+v", def)
	}

	// Second sweep: T500 now engaged — no double spawn (T222 compose).
	reps = s.frontierConsumeSweep(loopArgs, ledger)
	for _, r := range reps {
		if r.TargetID == "T500" && r.Action != FrontierConsumeSkip {
			t.Fatalf("second sweep T500: %+v", r)
		}
		if r.Action == FrontierConsumeSpawn {
			t.Fatalf("second sweep spawned again: %+v", r)
		}
	}
}

// PO unregistered → every ready leaf parks; nothing spawns (T129 exception path).
func TestFrontierConsumeSweepAssemblyPOMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(frontierConsumeTestLedger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:  s,
		Workdir: dir,
		Spawn: func(targetfile.FrontierLeaf, string, string) error {
			t.Fatal("must not spawn without registered PO")
			return nil
		},
	}, nil)
	for _, r := range reps {
		if r.TargetID == "T500" && (r.Action != FrontierConsumePark || r.Reason != FrontierReasonPOMissing) {
			t.Fatalf("T500: %+v", r)
		}
	}
}

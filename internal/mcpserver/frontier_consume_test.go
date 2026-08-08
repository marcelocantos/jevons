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

// 🎯T337: fixture like T7 (depends_on T5 set_aside) → park, not spawn.
func TestSweepFrontierConsumeSetAsideDepParkNotSpawn(t *testing.T) {
	spawned := 0
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{
				ID: "T7", Name: "Mobile app for Jevon",
				SetAsideDeps: []string{"T5"}, Cost: 20, Tags: []string{"visual"},
			},
			{ID: "T500", Name: "Ordinary ready Build leaf"},
		},
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 2,
		Spawn: func(poproactive.LeafObs, string) error {
			spawned++
			return nil
		},
	})
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	r7 := byID["T7"]
	if r7.Action != FrontierConsumePark || r7.Reason != FrontierReasonSetAsideDep {
		t.Fatalf("T7: action=%s reason=%s want park/skip_set_aside_dep (%+v)", r7.Action, r7.Reason, r7)
	}
	if !strings.Contains(r7.Err, "T5") {
		t.Fatalf("T7 park err should name set_aside dep: %q", r7.Err)
	}
	if byID["T500"].Action != FrontierConsumeSpawn {
		t.Fatalf("ordinary ready leaf must still spawn: %+v", byID["T500"])
	}
	if spawned != 1 {
		t.Fatalf("spawned=%d want 1 (T7 parked)", spawned)
	}
}

// 🎯T337: high-cost mobile parks unless unattended-safe / force-engage.
func TestSweepFrontierConsumeHighCostMobilePark(t *testing.T) {
	spawned := 0
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{ID: "T7a", Name: "Mobile app", Cost: 20, Tags: []string{"visual"}},
			{ID: "T7b", Name: "Mobile app", Cost: 20, Tags: []string{"visual", "unattended-safe"}},
			{ID: "T7c", Name: "Mobile app", Cost: 20, Tags: []string{"visual", "force-engage"}},
		},
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 5,
		Spawn: func(poproactive.LeafObs, string) error {
			spawned++
			return nil
		},
	})
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	if byID["T7a"].Action != FrontierConsumePark || byID["T7a"].Reason != FrontierReasonHighCostMobile {
		t.Fatalf("T7a: %+v", byID["T7a"])
	}
	if byID["T7b"].Action != FrontierConsumeSpawn {
		t.Fatalf("unattended-safe must spawn: %+v", byID["T7b"])
	}
	if byID["T7c"].Action != FrontierConsumeSpawn {
		t.Fatalf("force-engage must spawn: %+v", byID["T7c"])
	}
	if spawned != 2 {
		t.Fatalf("spawned=%d want 2", spawned)
	}
}

// 🎯T337 assembly: ledger T7→T5 set_aside is never auto-spawned.
func TestFrontierConsumeSweepAssemblySetAsideDep(t *testing.T) {
	const ledger = `
targets:
  T5:
    name: Auth parked
    status: set_aside
  T7:
    name: Mobile app for Jevon
    status: converging
    cost: 20
    value: 20
    tags:
    - visual
    depends_on:
    - T5
  T500:
    name: Ordinary ready leaf
    status: identified
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	var spawned []string
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:            s,
		Workdir:           dir,
		ParentPO:          "jevons-po",
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			spawned = append(spawned, leaf.ID)
			return reg.Register(claudia.AgentDef{
				Name: workerName, WorkDir: dir, SessionID: "spawned",
				Purpose: claudia.PurposeWork, Parent: parent, TargetID: leaf.ID,
			})
		},
	}, nil)
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	if r := byID["T7"]; r.Action != FrontierConsumePark || r.Reason != FrontierReasonSetAsideDep {
		t.Fatalf("T7 assembly: %+v", r)
	}
	if r := byID["T500"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("T500 assembly: %+v", r)
	}
	for _, id := range spawned {
		if id == "T7" {
			t.Fatal("T7 must not be spawned")
		}
	}
}

// 🎯T338: parent with active children parks; ready child leaf still spawns.
func TestSweepFrontierConsumeParentActiveChildrenParkNotSpawn(t *testing.T) {
	spawned := 0
	var spawnedIDs []string
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{
				ID: "T10", Name: "sqlpipe-based state sync",
				ActiveChildren: []string{"T10.2", "T10.3", "T10.6"},
				Cost:           13, Context: "needs CGO Peer",
			},
			{ID: "T10.2", Name: "Server Peer + owned tables", Cost: 8},
			{ID: "T500", Name: "Ordinary ready Build leaf"},
		},
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf poproactive.LeafObs, _ string) error {
			spawned++
			spawnedIDs = append(spawnedIDs, leaf.ID)
			return nil
		},
	})
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	r10 := byID["T10"]
	if r10.Action != FrontierConsumePark || r10.Reason != FrontierReasonParentActiveChildren {
		t.Fatalf("T10: action=%s reason=%s want park/skip_parent_with_active_children (%+v)",
			r10.Action, r10.Reason, r10)
	}
	if !strings.Contains(r10.Err, "parent-with-active-children") || !strings.Contains(r10.Err, "T10.2") {
		t.Fatalf("T10 park err should name parent-with-active-children + kids: %q", r10.Err)
	}
	if byID["T10.2"].Action != FrontierConsumeSpawn {
		t.Fatalf("ready child leaf must still spawn: %+v", byID["T10.2"])
	}
	if byID["T500"].Action != FrontierConsumeSpawn {
		t.Fatalf("ordinary ready leaf must still spawn: %+v", byID["T500"])
	}
	for _, id := range spawnedIDs {
		if id == "T10" {
			t.Fatal("T10 parent must not be spawned")
		}
	}
	if spawned != 2 {
		t.Fatalf("spawned=%d want 2 (parent parked)", spawned)
	}
}

// 🎯T338: high-infra sqlpipe/CGO parks unless unattended-safe / force-engage.
func TestSweepFrontierConsumeHighInfraPark(t *testing.T) {
	spawned := 0
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{ID: "T10a", Name: "sqlpipe state sync", Cost: 13, Context: "CGO Peer"},
			{ID: "T10b", Name: "sqlpipe state sync", Cost: 13, Tags: []string{"unattended-safe"}},
			{ID: "T10c", Name: "sqlpipe state sync", Cost: 13, Tags: []string{"force-engage"}},
		},
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 5,
		Spawn: func(poproactive.LeafObs, string) error {
			spawned++
			return nil
		},
	})
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	if byID["T10a"].Action != FrontierConsumePark || byID["T10a"].Reason != FrontierReasonHighInfra {
		t.Fatalf("T10a: %+v", byID["T10a"])
	}
	if byID["T10b"].Action != FrontierConsumeSpawn {
		t.Fatalf("unattended-safe must spawn: %+v", byID["T10b"])
	}
	if byID["T10c"].Action != FrontierConsumeSpawn {
		t.Fatalf("force-engage must spawn: %+v", byID["T10c"])
	}
	if spawned != 2 {
		t.Fatalf("spawned=%d want 2", spawned)
	}
}

// 🎯T338 assembly: ledger parent+active children never auto-spawns parent;
// ready child leaf still spawns.
func TestFrontierConsumeSweepAssemblyParentActiveChildren(t *testing.T) {
	const ledger = `
targets:
  T10:
    name: sqlpipe-based state sync
    status: converging
    cost: 13
    value: 20
    context: needs CGO Peer rebuild
  T10.2:
    name: Server Peer + owned tables
    status: identified
    cost: 8
  T10.3:
    name: Client path
    status: converging
  T500:
    name: Ordinary ready leaf
    status: identified
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	var spawned []string
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:            s,
		Workdir:           dir,
		ParentPO:          "jevons-po",
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			spawned = append(spawned, leaf.ID)
			return reg.Register(claudia.AgentDef{
				Name: workerName, WorkDir: dir, SessionID: "spawned",
				Purpose: claudia.PurposeWork, Parent: parent, TargetID: leaf.ID,
			})
		},
	}, nil)
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	if r := byID["T10"]; r.Action != FrontierConsumePark || r.Reason != FrontierReasonParentActiveChildren {
		t.Fatalf("T10 assembly: %+v", r)
	}
	if r := byID["T10.2"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("T10.2 ready child assembly: %+v", r)
	}
	if r := byID["T500"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("T500 assembly: %+v", r)
	}
	for _, id := range spawned {
		if id == "T10" {
			t.Fatal("T10 parent must not be spawned")
		}
	}
}

// 🎯T339: Not urgent / deferred voice context → park, not spawn.
func TestSweepFrontierConsumeDeferredNotUrgentParkNotSpawn(t *testing.T) {
	spawned := 0
	var spawnedIDs []string
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{
				ID: "T22", Name: "Voice traffic flows browser-to-Grok directly",
				Context: "Not urgent — laptop dev path works fine via the proxy.",
			},
			{
				ID: "T22b", Name: "Voice residual",
				Context: "deferred until iPad; later-device class.",
			},
			{
				ID: "T22c", Name: "Owner parked voice",
				Tags: []string{"owner-parked"},
			},
			{ID: "T500", Name: "Ordinary ready Build leaf"},
		},
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf poproactive.LeafObs, _ string) error {
			spawned++
			spawnedIDs = append(spawnedIDs, leaf.ID)
			return nil
		},
	})
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	r22 := byID["T22"]
	if r22.Action != FrontierConsumePark || r22.Reason != FrontierReasonDeferred {
		t.Fatalf("T22: action=%s reason=%s want park/skip_deferred (%+v)", r22.Action, r22.Reason, r22)
	}
	if byID["T22b"].Action != FrontierConsumePark || byID["T22b"].Reason != FrontierReasonDeferred {
		t.Fatalf("T22b: %+v", byID["T22b"])
	}
	if byID["T22c"].Action != FrontierConsumePark || byID["T22c"].Reason != FrontierReasonOwnerParked {
		t.Fatalf("T22c owner-parked: %+v", byID["T22c"])
	}
	if byID["T500"].Action != FrontierConsumeSpawn {
		t.Fatalf("ordinary ready leaf must still spawn: %+v", byID["T500"])
	}
	for _, id := range spawnedIDs {
		if id == "T22" || id == "T22b" || id == "T22c" {
			t.Fatalf("deferred leaf %s must not spawn", id)
		}
	}
	if spawned != 1 {
		t.Fatalf("spawned=%d want 1", spawned)
	}
}

// 🎯T339: force-engage / unattended-safe still spawn deferred leaves.
func TestSweepFrontierConsumeDeferredForceEngageSpawn(t *testing.T) {
	spawned := 0
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{ID: "T22a", Name: "Voice", Context: "Not urgent", ForceEngage: true},
			{ID: "T22b", Name: "Voice", Context: "Not urgent", Tags: []string{"unattended-safe"}},
			{ID: "T22c", Name: "Voice", Context: "Not urgent"},
		},
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 5,
		Spawn: func(poproactive.LeafObs, string) error {
			spawned++
			return nil
		},
	})
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	if byID["T22a"].Action != FrontierConsumeSpawn {
		t.Fatalf("force_engage must spawn: %+v", byID["T22a"])
	}
	if byID["T22b"].Action != FrontierConsumeSpawn {
		t.Fatalf("unattended-safe must spawn: %+v", byID["T22b"])
	}
	if byID["T22c"].Action != FrontierConsumePark || byID["T22c"].Reason != FrontierReasonDeferred {
		t.Fatalf("plain deferred must park: %+v", byID["T22c"])
	}
	if spawned != 2 {
		t.Fatalf("spawned=%d want 2", spawned)
	}
}

// 🎯T342: T28-shaped device-voice DSP parks; T27.5 hub leaf still spawns.
func TestSweepFrontierConsumeDeviceVoiceDSPParkNotSpawn(t *testing.T) {
	spawned := 0
	var spawnedIDs []string
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves: []poproactive.LeafObs{
			{
				ID:   "T28",
				Name: "Adaptive road-noise suppression trains on idle cabin audio and subtracts it during speech",
				Context: "The car cabin is the primary deployment and road noise is the dominant threat. " +
					"Slots into the VoicelabKit capture pipeline. iPad-in-car primary.",
			},
			{
				ID:   "T27.5",
				Name: "jevonsd ingests provider data feeds into an aggregated live model broadcast to clients",
				Context: "Feed failures degrade gracefully; hub never wedges on stalled provider feeds.",
				Tags:    []string{"providers", "feeds", "aggregation", "streaming"},
			},
		},
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf poproactive.LeafObs, _ string) error {
			spawned++
			spawnedIDs = append(spawnedIDs, leaf.ID)
			return nil
		},
	})
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	r28 := byID["T28"]
	if r28.Action != FrontierConsumePark || r28.Reason != FrontierReasonDeferred {
		t.Fatalf("T28: action=%s reason=%s want park/skip_deferred (%+v)", r28.Action, r28.Reason, r28)
	}
	if byID["T27.5"].Action != FrontierConsumeSpawn {
		t.Fatalf("T27.5 hub must still spawn: %+v", byID["T27.5"])
	}
	for _, id := range spawnedIDs {
		if id == "T28" {
			t.Fatal("T28 device-voice must not spawn")
		}
	}
	if spawned != 1 {
		t.Fatalf("spawned=%d want 1 (T27.5 only)", spawned)
	}
}

// 🎯T342 assembly: ledger T28-shaped parks; T27.5 may spawn.
func TestFrontierConsumeSweepAssemblyDeviceVoiceDSP(t *testing.T) {
	const ledger = `
targets:
  T28:
    name: Adaptive road-noise suppression trains on idle cabin audio and subtracts it during speech
    status: identified
    context: The car cabin is the primary deployment and road noise is the dominant threat. Slots into the VoicelabKit capture pipeline ahead of the VAD. iPad continuously samples cabin/road noise.
  T27.5:
    name: jevonsd ingests provider data feeds into an aggregated live model broadcast to clients
    status: identified
    context: Feed failures degrade gracefully — a slow or stalled provider surfaces as degraded status and never wedges the hub.
    tags:
      - providers
      - feeds
      - aggregation
      - streaming
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	var spawned []string
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:            s,
		Workdir:           dir,
		ParentPO:          "jevons-po",
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			spawned = append(spawned, leaf.ID)
			return reg.Register(claudia.AgentDef{
				Name: workerName, WorkDir: dir, SessionID: "spawned",
				Purpose: claudia.PurposeWork, Parent: parent, TargetID: leaf.ID,
			})
		},
	}, nil)
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	if r := byID["T28"]; r.Action != FrontierConsumePark || r.Reason != FrontierReasonDeferred {
		t.Fatalf("T28 assembly: %+v", r)
	}
	if r := byID["T27.5"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("T27.5 assembly must spawn: %+v", r)
	}
	for _, id := range spawned {
		if id == "T28" {
			t.Fatal("T28 must not be spawned")
		}
	}
}

// 🎯T339 assembly: ledger T22-shaped "Not urgent" never auto-spawns.
func TestFrontierConsumeSweepAssemblyDeferredNotUrgent(t *testing.T) {
	const ledger = `
targets:
  T22:
    name: Voice traffic flows browser-to-Grok directly
    status: identified
    context: Not urgent — laptop dev path works fine via the proxy. Raise once iPad becomes primary.
  T500:
    name: Ordinary ready leaf
    status: identified
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	var spawned []string
	reps := s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:            s,
		Workdir:           dir,
		ParentPO:          "jevons-po",
		MaxSpawnsPerCycle: 5,
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			spawned = append(spawned, leaf.ID)
			return reg.Register(claudia.AgentDef{
				Name: workerName, WorkDir: dir, SessionID: "spawned",
				Purpose: claudia.PurposeWork, Parent: parent, TargetID: leaf.ID,
			})
		},
	}, nil)
	byID := map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byID[r.TargetID] = r
	}
	if r := byID["T22"]; r.Action != FrontierConsumePark || r.Reason != FrontierReasonDeferred {
		t.Fatalf("T22 assembly: %+v", r)
	}
	if r := byID["T500"]; r.Action != FrontierConsumeSpawn {
		t.Fatalf("T500 assembly: %+v", r)
	}
	for _, id := range spawned {
		if id == "T22" {
			t.Fatal("T22 must not be spawned")
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

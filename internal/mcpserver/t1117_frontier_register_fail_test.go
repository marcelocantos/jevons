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
	"github.com/marcelocantos/jevons/internal/poproactive"
)

// 🎯T1117: 2026-10-21 19:25/19:35 — a frontier sweep over several ready
// leaves whose names carried reaped intent, against a registry/broker that
// refused every register (plan dest empty), touched six names in one
// cycle though the cap was 1, because a failed register attempt was free:
// only a SUCCESSFUL spawn incremented the per-cycle counter. Separately and
// worse, the owner found some of those failed-register names had actually
// been minted and left RUNNING at the broker — a live, token-consuming
// seat with no corresponding registry row the rest of the harness could
// see at all.

// TestT1117SweepBoundsAttemptsNotJustSuccesses pins the first half: the
// per-cycle cap must bound every register ATTEMPT, not just the successful
// ones. Six ready leaves, MaxSpawnsPerCycle=1, every attempt fails — at
// most one Spawn call is made, not six.
func TestT1117SweepBoundsAttemptsNotJustSuccesses(t *testing.T) {
	var leaves []poproactive.LeafObs
	for i := 0; i < 6; i++ {
		leaves = append(leaves, poproactive.LeafObs{
			ID:   "T" + string(rune('1'+i)) + "117",
			Name: "a ready leaf whose register always fails",
		})
	}
	calls := 0
	reps := SweepFrontierConsume(FrontierConsumeArgs{
		Leaves:            leaves,
		Now:               frontierNow(),
		PORegistered:      true,
		MaxSpawnsPerCycle: 1,
		Spawn: func(poproactive.LeafObs, string) error {
			calls++
			return errFake // every register fails, e.g. plan dest empty
		},
	})
	if calls > 1 {
		t.Fatalf("Spawn called %d times with MaxSpawnsPerCycle=1 and every\n"+
			"attempt failing — want at most 1: a failed register must count\n"+
			"against the per-cycle cap the same as a successful one, not be a\n"+
			"free retry against the next ready leaf", calls)
	}
	if calls == 0 {
		t.Fatal("Spawn never called — test setup is wrong, not proving anything")
	}
	spawnFailed, capacity := 0, 0
	for _, r := range reps {
		switch r.Reason {
		case FrontierReasonSpawnFailed:
			spawnFailed++
		case FrontierReasonCapacity:
			capacity++
		}
	}
	if spawnFailed != calls {
		t.Fatalf("park_spawn_failed reports = %d, want %d (one per actual attempt)", spawnFailed, calls)
	}
	if spawnFailed+capacity != len(leaves) {
		t.Fatalf("reports = %+v, want every leaf parked spawn_failed (attempted) or capacity (never attempted)", reps)
	}
}

// TestT1117FailedRegisterDoesNotPersistGhostAutoStartRow pins the second,
// more serious half: a register that fails at the admission check (plan
// dest empty) for a brand-new name must leave no registry row behind at
// all. A ghost AutoStart=true row from a half-completed register is bait
// for fleet_health's dead-AutoStart recovery sweep, which would later
// launch it for real — a live broker seat admission never approved.
func TestT1117FailedRegisterDoesNotPersistGhostAutoStartRow(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	now := time.Now()
	hot := func(p string) planusage.Backend {
		used, rem := 100.0, 0.0
		resets := now.Add(48 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Backend{Provider: p, Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	}
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{hot("claude"), hot("codex"), hot("grok")}}
	})

	const name = "jv-t1117-ghost"
	if reg.Def(name) != nil {
		t.Fatalf("precondition: %q already registered", name)
	}

	err = s.spawnFrontierWorker(name, dir, "jevons-po", "T1117", "brief")
	if err == nil || !strings.Contains(err.Error(), "plan dest empty") {
		t.Fatalf("spawnFrontierWorker with every plan hot = %v, want a plan dest empty refusal", err)
	}

	if d := reg.Def(name); d != nil {
		t.Fatalf("register refusal left a ghost registry row behind: %+v\n"+
			"(AutoStart=%v) — this is exactly the bait fleet_health's dead-"+
			"AutoStart recovery sweep would later launch for real, with no "+
			"completed registration behind it", d, d.AutoStart)
	}
}

// TestT1117FailedRegisterRestoresPriorIntent pins the ledger-intent half of
// the same bug for a name that WAS already registered (the 🎯T560
// kill-with-descendants-preserved shape): ensureFreshStartIntent flips a
// Reaped name to Working before register runs, and when register then
// fails, nothing put the intent back — a stopped-row-with-working-intent
// lie that looks, to every other reader (including the 🎯T984 stand-down
// sweep, which has nothing to stand down since nothing launched), like a
// seat that is just fine.
func TestT1117FailedRegisterRestoresPriorIntent(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	const name = "jv-t1117-reaped"
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
	now := time.Now()
	hot := func(p string) planusage.Backend {
		used, rem := 100.0, 0.0
		resets := now.Add(48 * time.Hour)
		lim := planusage.DefaultWeeklyWindowSeconds
		return planusage.Backend{Provider: p, Status: planusage.StatusAvailable,
			Windows: []planusage.Window{{Name: planusage.WindowWeekly, UsedPercent: &used, RemainingPercent: &rem, ResetsAt: &resets, LimitWindowSeconds: &lim}}}
	}
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{hot("claude"), hot("codex"), hot("grok")}}
	})

	if got := s.fleetIntent().AgentState(name); got != fleetintent.Reaped {
		t.Fatalf("precondition: intent = %q, want reaped", got)
	}

	err = s.spawnFrontierWorker(name, dir, "jevons-po", "T1117", "brief")
	if err == nil || !strings.Contains(err.Error(), "plan dest empty") {
		t.Fatalf("spawnFrontierWorker with every plan hot = %v, want a plan dest empty refusal", err)
	}

	if got := s.fleetIntent().AgentState(name); got != fleetintent.Reaped {
		t.Fatalf("intent after failed register = %q, want reaped restored "+
			"(a register that never launched a process must not leave the "+
			"ledger claiming the name is Working, 🎯T1117)", got)
	}

	// Companion to the acceptance's third clause: with intent correctly
	// restored to Reaped (stood-down) and nothing running, the 🎯T984
	// stand-down sweep has nothing against this name to report.
	found := s.stopSeatsAgainstIntent(nil)
	for _, v := range found {
		if v.Name == name {
			t.Fatalf("🎯T984 stand-down sweep found something to stop for "+
				"%q after a failed register that launched nothing: %+v", name, v)
		}
	}
}

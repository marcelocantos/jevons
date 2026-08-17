// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T499 acceptance oracle: two ready leaves differing only by age → the
// per-cycle pick goes to the newer; the older is STILL READY afterward —
// parked park_capacity this sweep, spawned once the newer is engaged. A
// bias among ready leaves, never a next-ticket queue (🎯T262.1).
//
// The older leaf carries the LOWER id, so the pre-🎯T499 natural-id
// ascending iteration would pick it and fail this test; date-beats-id in
// both directions is pinned by the targetfile unit tests.
const t499TestLedger = `
targets:
  T600:
    name: Long-lived ready leaf
    status: identified
    discovered: 2026-01-01
  T601:
    name: Recently filed ready leaf
    status: identified
    discovered: 2026-08-17
`

func TestT499SweepPicksNewerReadyLeafOlderStaysReady(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t499TestLedger), 0o644); err != nil {
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

	ledger, err := OpenFrontierSpawnLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	loopArgs := FrontierConsumeLoopArgs{
		Server:   s,
		Workdir:  dir,
		ParentPO: "jevons-po",
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
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
	// The single per-cycle spawn slot goes to the newer filing.
	if r := byTarget["T601"]; r.Action != FrontierConsumeSpawn || r.Worker != "jv-t601-auto" {
		t.Fatalf("newer leaf T601: %+v", r)
	}
	// The older leaf is parked for capacity — still ready, not skipped,
	// closed, or dropped from the sweep.
	if r, ok := byTarget["T600"]; !ok || r.Action != FrontierConsumePark || r.Reason != FrontierReasonCapacity {
		t.Fatalf("older leaf T600 must park park_capacity, got %+v (present=%v)", r, ok)
	}

	// Next sweep: newer leaf engaged → the older leaf gets the slot. Recency
	// deprioritizes; it never starves an eligible leaf into a dead queue.
	reps = s.frontierConsumeSweep(loopArgs, ledger)
	byTarget = map[string]FrontierConsumeReport{}
	for _, r := range reps {
		byTarget[r.TargetID] = r
	}
	if r := byTarget["T601"]; r.Action != FrontierConsumeSkip || r.Reason != FrontierReasonEngaged {
		t.Fatalf("second sweep T601: %+v", r)
	}
	if r := byTarget["T600"]; r.Action != FrontierConsumeSpawn || r.Worker != "jv-t600-auto" {
		t.Fatalf("second sweep must spawn the older leaf T600: %+v", r)
	}
}

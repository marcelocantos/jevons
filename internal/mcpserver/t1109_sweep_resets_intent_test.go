// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T1109 (reopened): the prior fix (566cf9e8) only reset a previously-reaped
// name's intent on jevons_agent_start's own handler path (handleAgentStart).
// The frontier sweep's auto-spawn does NOT go through that handler — it
// calls spawnFrontierWorker, which registers and launches directly — so a
// sweep re-mint of a once-killed-with-descendants-preserved (🎯T560) name
// left intent at Reaped under a live process, and the 🎯T984 stand-down
// sweep stopped it again within its next pass. This drives the frontier
// sweep's OWN spawn path (spawnFrontierWorker, the function
// frontierConsumeSweep's Spawn callback calls in production) against a
// preserved-reaped row and checks the fix landed there too.
func TestT1109SweepSpawnPathResetsReapedIntent(t *testing.T) {
	dir := t.TempDir()
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

	const name = "jv-t1109-auto"
	workdir := t.TempDir()

	// The row 🎯T560's kill-with-descendants-preserved leaves behind: still
	// registered, stopped, and stamped Reaped — the exact shape a sweep
	// re-mint sees for a name that was killed earlier.
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workdir, SessionID: "s-" + name,
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
	t.Cleanup(reg.StopAll)

	if got := s.fleetIntent().AgentState(name); got != fleetintent.Reaped {
		t.Fatalf("precondition: intent = %q, want reaped", got)
	}

	// This is the sweep's own spawn primitive — frontierConsumeSweep's
	// production Spawn callback calls exactly this function (not
	// handleAgentStart). If the fix only lives in handleAgentStart, this
	// call registers+launches a live process while leaving intent at
	// Reaped, which is the 🎯T1109 bug.
	if err := s.spawnFrontierWorker(name, workdir, "jevons-po", "T1109", ""); err != nil {
		t.Fatalf("sweep spawn path refused a fresh start under a preserved killed row: %v", err)
	}

	if got := s.fleetIntent().AgentState(name); got != fleetintent.Working {
		t.Fatalf("intent after sweep spawn = %q, want working (fresh start under a previously reaped name)", got)
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

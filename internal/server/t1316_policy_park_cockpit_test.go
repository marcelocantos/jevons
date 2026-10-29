// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T1316: a stopped overseer that plan policy parked is not relaunched by the
// cockpit. On claudia v0.77.0 J20 failed because the cockpit relaunched the
// parked overseer onto the exhausted plan 0.7 s after the park.
func TestT1316PlanCockpitLeavesAPolicyParkedOverseerDown(t *testing.T) {
	o := cockpitObs{Registered: true, ProcAlive: false, PolicyParked: true}
	if got := planCockpit(o, 0, 8, 0); got != cockpitPolicyParked {
		t.Fatalf("policy-parked stopped overseer: phase %d, want cockpitPolicyParked", got)
	}
	o.PolicyParked = false
	if got := planCockpit(o, 0, 8, 0); got != cockpitLaunch {
		t.Fatalf("unparked stopped overseer: phase %d, want launch", got)
	}
}

// The park is read from fleet intent, and only a plan-policy park counts: an
// owner's park of the overseer is not this case.
func TestT1316ObserveCockpitReadsOnlyAPlanPolicyPark(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "jevons", WorkDir: dir, SessionID: "s", Purpose: claudia.PurposeOverseer}); err != nil {
		t.Fatal(err)
	}
	seatstate.ObserveStopped(reg, "jevons")
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	s.SetFleetIntentSource(store.Snapshot)

	if err := store.SetAgent("jevons", fleetintent.Parked, fleetintent.PlanPolicyActor, "resolve: every plan exhausted", time.Now()); err != nil {
		t.Fatal(err)
	}
	o := s.ObserveCockpit()
	if !o.PolicyParked || o.PolicyParkReason != "resolve: every plan exhausted" {
		t.Fatalf("plan-policy park not observed: %+v", o)
	}
	if got := planCockpit(o, 0, 8, 0); got != cockpitPolicyParked {
		t.Fatalf("phase %d, want cockpitPolicyParked", got)
	}

	if err := store.SetAgent("jevons", fleetintent.Parked, "owner", "owner stood it down", time.Now()); err != nil {
		t.Fatal(err)
	}
	if o := s.ObserveCockpit(); o.PolicyParked {
		t.Fatal("an owner's park was read as a plan-policy park")
	}
}

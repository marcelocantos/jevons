// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T953: /api/agents must not report running: true while grant_held blocks relaunch.
func TestT953GrantHeldPinForcesStoppedFleetRow(t *testing.T) {
	t.Cleanup(func() { fleet.ClearGrantHeldRelaunch("jv-t953-api") })
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.EnsureAgentWithParent("jv-t953-api", "/tmp/w", "", "jevons-po", true); err != nil {
		t.Fatal(err)
	}
	a := seatstate.RegistryAuthority(reg)
	a.Observe(seatstate.Observation{
		Name: "jv-t953-api", Alive: seatstate.Yes, LocalAlive: seatstate.Yes,
		Source: "test", QueueDepth: seatstate.QueueUnknown,
	})
	a.Observe(seatstate.Observation{
		Name: "jv-t953-api", BrokerAlive: seatstate.Yes, BrokerOwned: seatstate.No,
		Source: "broker.seats", QueueDepth: seatstate.QueueUnknown,
	})
	fleet.NoteGrantHeldRelaunch("jv-t953-api")
	rows := listFleetAgents(reg)
	var found bool
	for _, r := range rows {
		if r.Name != "jv-t953-api" {
			continue
		}
		found = true
		if r.Running || r.Status == "running" {
			t.Fatalf("row = %+v, want stopped/not running", r)
		}
	}
	if !found {
		t.Fatal("jv-t953-api missing from fleet list")
	}
}

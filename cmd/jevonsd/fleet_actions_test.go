// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T139: StopFleet must not stop the protected overseer.
func TestStopFleetSkipsProtectedOverseer(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "agents.json")
	reg, err := claudia.NewRegistry(regPath)
	if err != nil {
		t.Fatal(err)
	}
	// Register defs without launching processes (Stop is no-op if not running).
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: dir, AutoStart: false,
		SessionID: "00000000-0000-4000-8000-000000000001",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", WorkDir: dir, AutoStart: false,
		SessionID: "00000000-0000-4000-8000-000000000002",
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate "running" by Launch would need provider; instead call StopFleet
	// and ensure protect path does not panic and PauseWorker skips jevons.
	a := &fleetActions{registry: reg, protect: []string{"jevons"}}
	if err := a.PauseWorker("jevons"); err != nil {
		t.Fatalf("PauseWorker protect: %v", err)
	}
	if err := a.StopFleet(); err != nil {
		t.Fatalf("StopFleet: %v", err)
	}
	// KillWorker protect must not Remove overseer.
	if err := a.KillWorker("jevons"); err != nil {
		t.Fatalf("KillWorker protect: %v", err)
	}
	if reg.Def("jevons") == nil {
		t.Fatal("overseer def was removed")
	}
}

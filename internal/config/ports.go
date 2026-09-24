// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"path/filepath"
)

// DevelopmentPort is the development bind (:13705, 🎯T572).
// After 🎯T540.2 this port serves the React cockpit from ui/dist.
const DevelopmentPort = 13705

// VanillaPort is the vanilla web/ supervisor program (🎯T540.4).
// Reference-only; journeys must not bind or adopt it. The in-process
// sidecar is opt-in (`-vanilla-port`) so it cannot fight the agent.
const VanillaPort = 13706

// JourneyPort is the default isolate bind (🎯T79 / 🎯T526).
// A second jevonsd on this port must never share development ~/.jevons state.
const JourneyPort = 13715

// RefuseVanillaPortAsPrimary refuses using the vanilla sidecar port as
// the daemon's primary listen. That port is reserved for the vanilla
// reference cockpit sitting beside React on DevelopmentPort.
func RefuseVanillaPortAsPrimary(port int) error {
	if port != VanillaPort {
		return nil
	}
	return fmt.Errorf("refusing port %d as primary listen (reserved for vanilla sidecar after 🎯T540.2); development React is :%d",
		VanillaPort, DevelopmentPort)
}

// IsDailyStateDir reports whether dir resolves to the default development
// state root (~/.jevons). Used to keep isolates off the owner's registry
// (🎯T503, 🎯T526). Legacy name; speech is development (🎯T572).
func IsDailyStateDir(dir string) bool {
	if dir == "" {
		return false
	}
	want, err := filepath.Abs(Default().StateDir)
	if err != nil {
		return false
	}
	got, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	if resolved, err := filepath.EvalSymlinks(got); err == nil {
		got = resolved
	}
	return got == want
}

// RefuseJourneyDevState refuses the journey default port when state_dir
// is the development root. Without this, `jevonsd -port 13715 -workdir <repo>`
// loads ~/.jevons/config.yaml and shares workers.db / agents.json with
// :13705 — the 2026-08-19 split-brain that minted J20 fixtures into the
// development registry (🎯T526).
func RefuseJourneyDevState(port int, stateDir string) error {
	if port != JourneyPort {
		return nil
	}
	if !IsDailyStateDir(stateDir) {
		return nil
	}
	return fmt.Errorf("refusing port %d with development state_dir %s; journey isolate needs an explicit throwaway state_dir (not ~/.jevons)",
		JourneyPort, stateDir)
}

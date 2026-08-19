// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"path/filepath"
)

// DailyPort is the live owner-driver bind (Universe A).
const DailyPort = 13705

// JourneyPort is the default Universe B isolate bind (🎯T79 / 🎯T526).
// A second jevonsd on this port must never share daily ~/.jevons state.
const JourneyPort = 13715

// IsDailyStateDir reports whether dir resolves to the default daily state
// root (~/.jevons). Used to keep isolates off the owner's registry (🎯T503,
// 🎯T526).
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

// RefuseJourneyDailyState refuses the journey default port when state_dir
// is the daily root. Without this, `jevonsd -port 13715 -workdir <repo>`
// loads ~/.jevons/config.yaml and shares workers.db / agents.json with
// :13705 — the 2026-08-19 split-brain that minted J20 fixtures into the
// daily registry (🎯T526).
func RefuseJourneyDailyState(port int, stateDir string) error {
	if port != JourneyPort {
		return nil
	}
	if !IsDailyStateDir(stateDir) {
		return nil
	}
	return fmt.Errorf("refusing port %d with daily state_dir %s; journey isolate needs an explicit throwaway state_dir (not ~/.jevons)",
		JourneyPort, stateDir)
}

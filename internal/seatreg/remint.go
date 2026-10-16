// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

import (
	"path/filepath"

	"github.com/marcelocantos/claudia"
)

// GrantsFile is the broker daemon registry basename under the claudia
// state directory. The live fleet lives in ~/.jevons/agents.json;
// leftover AutoStart rows here are not that fleet (🎯T866.6).
const GrantsFile = "grants.json"

// GrantsPath is grants.json under the broker state directory.
func GrantsPath(stateDir string) string {
	return filepath.Join(stateDir, GrantsFile)
}

// ParkNonFleetAutoStart turns off AutoStart for grant rows that are not
// live fleet names, so a broker bounce does not relaunch leftover
// vendor-CLI seats as if they were the fleet (🎯T866.6).
func ParkNonFleetAutoStart(grants, fleet *claudia.Registry) (int, error) {
	if grants == nil {
		return 0, nil
	}
	live := map[string]bool{}
	if fleet != nil {
		for _, row := range fleet.List() {
			live[row.Name] = true
		}
	}
	n := 0
	for _, row := range grants.List() {
		if !row.AutoStart || live[row.Name] {
			continue
		}
		row.AutoStart = false
		if err := grants.Register(row); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

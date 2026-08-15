// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"fmt"

	"github.com/marcelocantos/claudia"
)

// ReattachFleet is the T40.2 return: every jevonsd boot adopts leftover
// processes, then Launch-es (resumes) what exited. Launch itself still
// only creates. Leftovers are not reaped.
//
// Upgrade handoff is no longer what chooses the start method — it is
// only consumed so a later drain start is not mistaken for an upgrade.
func ReattachFleet(reg *claudia.Registry) {
	if reg == nil {
		return
	}
	reg.StartAllPreferAdopt()
}

// SessionSnapshot is name → session_id for every registered row.
func SessionSnapshot(reg *claudia.Registry) map[string]string {
	out := map[string]string{}
	if reg == nil {
		return out
	}
	for _, d := range reg.List() {
		out[d.Name] = d.SessionID
	}
	return out
}

// SessionSnapshotFromFile loads a claudia agents.json the same way a
// restarted daemon does and returns the session snapshot. Journeys and
// hermetics share this so neither reimplements the persist format.
func SessionSnapshotFromFile(path string) (map[string]string, error) {
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		return nil, fmt.Errorf("session snapshot: %w", err)
	}
	return SessionSnapshot(reg), nil
}

// SessionDrift names rows whose session_id changed (or vanished). An
// empty result is the T40.2 pass: bounce kept every conversation.
func SessionDrift(before, after map[string]string) []string {
	var out []string
	for name, sid := range before {
		got, ok := after[name]
		if !ok {
			out = append(out, name+" vanished")
			continue
		}
		if got != sid {
			out = append(out, name+": "+sid+" → "+got)
		}
	}
	return out
}

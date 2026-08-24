// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"fmt"

	"github.com/marcelocantos/claudia"
)

// ReattachFleet is the T40.2 return: every jevonsd boot adopts leftover
// processes, then Launch-es (resumes) what exited. Launch itself still
// only creates. Leftovers are not reaped. The returned names reminted
// a new session_id (🎯T545.1); callers must not full_brief those seats.
//
// Upgrade handoff is no longer what chooses the start method — it is
// only consumed so a later drain start is not mistaken for an upgrade.
func ReattachFleet(reg *claudia.Registry) []string {
	if reg == nil {
		return nil
	}
	before := SessionSnapshot(reg)
	// Cursor ACP leftovers cannot be adopted. Reap them before Launch
	// so the successor opens exactly one client per store.db (🎯T541.1).
	ReapCursorFleetLeftovers(reg)
	reapOrphanCursorACP()
	reg.StartAllPreferAdopt()
	return SessionDriftNames(before, SessionSnapshot(reg))
}

// reapOrphanCursorACP is a seam so hermetics never signal live leftovers.
var reapOrphanCursorACP = claudia.ReapOrphanCursorACP

// ReapCursorFleetLeftovers kills leftover writers on every registered
// Cursor session store (persisted ConnectPID + anyone holding store.db).
func ReapCursorFleetLeftovers(reg *claudia.Registry) {
	if reg == nil {
		return
	}
	for _, d := range reg.List() {
		if d.Provider != claudia.ProviderCursor {
			continue
		}
		claudia.ReapCursorACPLeftovers(d.SessionID, d.ConnectPID)
	}
}

// StopNonAdoptable stops Cursor (and other stdio-owned) seats on upgrade
// exit. Grok connect-mode and Claude tmux stay running for reattach.
func StopNonAdoptable(reg *claudia.Registry) int {
	if reg == nil {
		return 0
	}
	n := 0
	for _, d := range reg.List() {
		h := Handle{
			Name:         d.Name,
			SessionID:    d.SessionID,
			Provider:     string(d.Provider),
			ConnectURL:   d.ConnectURL,
			PID:          d.ConnectPID,
			TmuxWindowID: "",
		}
		proc := reg.Get(d.Name)
		if proc != nil {
			if p := proc.PID(); p > 0 {
				h.PID = p
			}
			if u := proc.ConnectURL(); u != "" {
				h.ConnectURL = u
			}
			if w := adoptiveTmuxWindowID(proc.WindowID()); w != "" {
				h.TmuxWindowID = w
			}
		}
		if !ShouldStopOnUpgrade(h, proc != nil) {
			continue
		}
		reg.Stop(d.Name)
		n++
	}
	return n
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

// SessionDriftNames is the remint set: rows whose session_id changed.
// Vanished names are not remints (T545.1 full_brief skip uses this).
func SessionDriftNames(before, after map[string]string) []string {
	var out []string
	for name, sid := range before {
		got, ok := after[name]
		if ok && got != sid {
			out = append(out, name)
		}
	}
	return out
}

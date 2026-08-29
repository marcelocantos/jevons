// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/keepgoing"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// workAgentsBoundOnTarget is engagement for the consume sweep (🎯T566.3):
// a live process or a registered name with no handle is bound (do not
// mint a second pane). A handle that exists and is not Alive is a remint
// candidate, not engaged.
func workAgentsBoundOnTarget(reg *claudia.Registry, targetID, scopeWorkdir, excludeName string) []string {
	var bound []string
	for _, name := range workAgentsEngagedOnTarget(reg, targetID, scopeWorkdir, excludeName) {
		proc := reg.Get(name)
		if proc == nil || proc.Alive() {
			bound = append(bound, name)
		}
	}
	return bound
}

func keepgoingSeats(reg *claudia.Registry, workdir string) []keepgoing.Seat {
	if reg == nil {
		return nil
	}
	wantLedger := targetfile.LedgerKey(workdir)
	var out []keepgoing.Seat
	for _, d := range reg.List() {
		tid := normalizeAgentTargetID(d.TargetID)
		if tid == "" {
			continue
		}
		if !targetfile.SameLedger(wantLedger, targetfile.LedgerKey(d.WorkDir)) {
			continue
		}
		p := strings.TrimSpace(d.Purpose)
		if p == "" {
			p = claudia.PurposeWork
		}
		if p != claudia.PurposeWork {
			continue
		}
		running := false
		if proc := reg.Get(d.Name); proc != nil {
			running = proc.Alive()
		}
		out = append(out, keepgoing.Seat{Name: d.Name, TargetID: tid, Running: running})
	}
	return out
}

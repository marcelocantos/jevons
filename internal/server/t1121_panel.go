// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetpanel"
)

// SetFleetIntentSource wires the durable intent ledger for 🎯T1121 panel
// classification on GET /api/agents. Nil resolves every seat to working.
func (s *Server) SetFleetIntentSource(fn func() fleetintent.Snapshot) {
	s.fleetIntentSource = fn
}

func (s *Server) decoratePanelClass(agents []agentInfo) []agentInfo {
	s.mu.RLock()
	reg := s.registry
	intentFn := s.fleetIntentSource
	s.mu.RUnlock()
	var snap fleetintent.Snapshot
	if intentFn != nil {
		snap = intentFn()
	}
	for i := range agents {
		autoStart := false
		if reg != nil {
			if d := reg.Def(agents[i].Name); d != nil {
				autoStart = d.AutoStart
			}
		}
		rec := fleetintent.Record{}
		if snap.Agents != nil {
			rec = snap.Agents[agents[i].Name]
		}
		row := fleetpanel.Classify(fleetpanel.Input{
			Running:    agents[i].Running,
			Starting:   agents[i].Starting,
			AutoStart:  autoStart,
			Intent:     snap.AgentState(agents[i].Name),
			Record:     rec,
			StopReason: agents[i].StopReason,
			StoppedAt:  agents[i].StoppedAt,
		})
		agents[i].Class = string(row.Class)
		agents[i].Visible = row.Visible
		agents[i].PanelLine = row.Line
	}
	return agents
}

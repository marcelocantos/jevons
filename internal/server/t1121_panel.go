// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetpanel"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// SetFleetIntentSource wires the durable intent ledger for 🎯T1121 panel
// classification on GET /api/agents. Nil resolves every seat to working.
func (s *Server) SetFleetIntentSource(fn func() fleetintent.Snapshot) {
	s.fleetIntentSource = fn
}

// SetAutoSeatHoldReader uses the same policy as the daemon's revival paths.
// A paused auto worker is not a dead durable fault.
func (s *Server) SetAutoSeatHoldReader(fn func(string) string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoSeatHoldReader = fn
}

func (s *Server) decoratePanelClass(agents []agentInfo) []agentInfo {
	s.mu.RLock()
	reg := s.registry
	intentFn := s.fleetIntentSource
	holdFn := s.autoSeatHoldReader
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
		processUp := agents[i].Running || agents[i].Starting
		unobserved := false
		if !processUp && reg != nil {
			// A positive observation only: Unknown is not a live process
			// (see mcpserver.seatProcessUp), and not a stop either.
			switch seatstate.ReadRegistry(reg, agents[i].Name).Alive {
			case seatstate.Yes:
				processUp = true
			case seatstate.Unknown:
				unobserved = true
			}
		}
		rec := fleetintent.Record{}
		if snap.Agents != nil {
			rec = snap.Agents[agents[i].Name]
		}
		intent := snap.AgentState(agents[i].Name)
		autoHold := ""
		if holdFn != nil {
			autoHold = holdFn(agents[i].Name)
		}
		row := fleetpanel.Classify(fleetpanel.Input{
			Running:    agents[i].Running,
			Starting:   agents[i].Starting,
			Unobserved: unobserved,
			AutoStart:  autoStart,
			AutoHold:   autoHold,
			Intent:     intent,
			Record:     rec,
			StopReason: agents[i].StopReason,
			StoppedAt:  agents[i].StoppedAt,
		})
		agents[i].Class = string(row.Class)
		agents[i].Visible = row.Visible
		agents[i].PanelLine = row.Line
		if agents[i].PhaseHealth != "" {
			if agents[i].PanelLine != "" {
				agents[i].PanelLine += "; "
			}
			agents[i].PanelLine += agents[i].PhaseHealth
		}
		if st := fleetpanel.ServedListStatus(processUp, agents[i].Starting, intent, agents[i].Status); st != agents[i].Status {
			agents[i].Status = st
			agents[i].Phase = st
		}
	}
	return agents
}

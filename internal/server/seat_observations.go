// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/seatactivity"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// ObserveSeatMetadata is called by the slow observation feed, never by a
// control read. Desired pins/defaults are not observations of a running model.
func (s *Server) ObserveSeatMetadata(d claudia.AgentDef) {
	s.mu.RLock()
	progress, models, roots := s.agentProgress, s.fleetModels, s.transcriptRoots
	s.mu.RUnlock()
	a := s.seats.Load()
	if a == nil {
		return
	}
	observeSeatModel(a, d, progress, models)
	activity := seatactivity.Lookup(seatactivity.Query{Name: d.Name, Provider: d.Provider, SessionID: d.SessionID, WorkDir: d.WorkDir, Roots: roots})
	if activity.Verdict == seatactivity.VerdictKnown {
		a.Observe(seatstate.Observation{Name: d.Name, ForSession: d.SessionID, LastActivity: activity.LastMove, QueueDepth: seatstate.QueueUnknown, Source: "transcript.activity"})
	}
}

func observeSeatModel(a *seatstate.Authority, d claudia.AgentDef, progress *AgentProgressHub, models *fleetModelResolver) {
	model := ""
	if progress != nil {
		progress.SyncEpoch(d.Name, d.SessionID)
		model = strings.TrimSpace(progress.Get(d.Name).Model)
	}
	if model == syntheticModel || !modelFitsProvider(string(d.Provider), model) {
		model = ""
		if progress != nil {
			progress.ClearModel(d.Name)
		}
	}
	if model == "" {
		model = models.Model(d.Provider, d.WorkDir, d.SessionID)
	}
	if model == "" && d.Provider == claudia.ProviderCursor && d.ConnectPID > 0 {
		if m, pid, ok := fleet.CursorStartedModel(d.SessionID); ok && pid == d.ConnectPID {
			model = m
		}
	}
	if model == "" || !modelFitsProvider(string(d.Provider), model) {
		return
	}
	a.Observe(seatstate.Observation{Name: d.Name, ForSession: d.SessionID, Provider: string(d.Provider),
		Model: model, QueueDepth: seatstate.QueueUnknown, Source: "session.model"})
}

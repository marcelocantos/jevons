// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetpanel"
)

func agentListShowHistory(args map[string]any) bool {
	return boolArg(args["history"]) || boolArg(args["all"])
}

func (s *Server) panelClassForDef(d claudia.AgentDef, alive, starting bool) fleetpanel.Row {
	snap := s.fleetIntent()
	rec := fleetintent.Record{}
	if snap.Agents != nil {
		rec = snap.Agents[d.Name]
	}
	stopReason := ""
	stoppedAt := ""
	if reason, at, ok := s.SeatStopReason(d.Name); ok {
		stopReason = reason
		stoppedAt = at.UTC().Format(time.RFC3339)
	}
	return fleetpanel.Classify(fleetpanel.Input{
		Running:    alive,
		Starting:   starting,
		AutoStart:  d.AutoStart,
		Intent:     snap.AgentState(d.Name),
		Record:     rec,
		StopReason: stopReason,
		StoppedAt:  stoppedAt,
	})
}

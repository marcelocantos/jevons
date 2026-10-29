// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetpanel"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

func agentListShowHistory(args map[string]any) bool {
	return boolArg(args["history"]) || boolArg(args["all"])
}

// reapedLimit is the reaped-section bound for one jevons_agent_list call.
func reapedLimit(showHistory bool) int {
	if showHistory {
		return 0
	}
	return reapedListLimit
}

// seatProcessUp is true when the list should treat the seat as having a live
// process for 🎯T1127 intent-fault surfacing. A fault needs a positive
// observation: Unknown is every seat in the minute after a restart, before
// the broker sweep answers (🎯T1157), and treating it as up painted 145
// stopped seats "should not be running" on 2026-10-22.
func (s *Server) seatProcessUp(name string, alive, starting bool) bool {
	if alive || starting {
		return true
	}
	if st, ok := s.Seats().Get(name); ok && st.Alive == seatstate.Yes {
		return true
	}
	return false
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
	st, _ := s.Seats().Get(d.Name)
	return fleetpanel.Classify(fleetpanel.Input{
		Running:    alive,
		Starting:   starting,
		Unobserved: !alive && !starting && st.Alive == seatstate.Unknown,
		AutoStart:  d.AutoStart,
		AutoHold:   s.autoSeatHold(d.Name),
		Intent:     snap.AgentState(d.Name),
		Record:     rec,
		StopReason: stopReason,
		StoppedAt:  stoppedAt,
	})
}

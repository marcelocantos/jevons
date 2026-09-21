// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "log/slog"

// Reconcile is the one pass that decides what to do about the fleet
// (🎯T766.3).
//
// Until 2026-09-21 the same work was reached by three routes on two
// schedules: the cockpit's fleet hooks fired SweepFleetHealth and the idle
// nudge sweep every 30 seconds, runIdlePressureLoop fired an overlapping
// bundle on its own 60-second ticker, and the sentinel's repair action called
// three of the same entry points back to back. In one minute the dead-agent
// sweep could judge a seat four times and the idle-pressure actuator three,
// each route with its own notion of order. docs/fleet-census.md named the
// pair — "cockpit fleet hooks: delete, it exists to avoid a separate 1m loop
// that still runs" — and this replaces both with one pass on one schedule.
//
// Every member runs exactly once, in this order, and nothing else calls
// them as a bundle. The order is load-bearing and inherited: queues are
// swept before anything can relaunch a rescuer (🎯T418), seat load is
// anchored while roots are alive and reaped after the dead-agent sweep
// (🎯T708), and pressure comes last so it acts on what the sweeps left.
//
// Guards (fleet intent, host load, capacity) are still applied inside the
// individual actuators; lifting them into this pass is the next slice.
func (s *Server) Reconcile() {
	if s == nil || s.registry == nil {
		return
	}
	overseer := s.overseerName()

	s.TrackSeatLoad()
	s.SweepOrphanPanes()

	s.SweepSendBacklogs()
	s.SweepHandovers()
	s.reportFleetMuteIfNeeded()

	if reps := s.sweepDeadAccountedWith(overseer, s.fleetIntent()); len(reps) > 0 {
		slog.Info("reconcile: fleet health", "report", FormatDeadAgentReport(reps))
	}
	s.ReapLostSeats()
	s.SweepSeatLoad()
	s.SweepPostReapCommits()
	s.sweepBornStuck()

	s.runFleetRecoverSweep(false)
	s.TriggerIdlePressureSweep()
	s.FlushWakeBatches()
}

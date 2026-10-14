// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
)

// Reasons an auto-spawned seat is held (🎯T1066).
const (
	// AutoHoldPaused: config frontier_consume.disabled — the owner's
	// stabilization pause on frontier-consume's own seats.
	AutoHoldPaused = "frontier_consume_disabled"
	// AutoHoldTargetAchieved: the ledger reads the seat's target as closed
	// (achieved, or owner-assigned). Its work is over; nothing should wake it.
	AutoHoldTargetAchieved = "target_achieved"
)

// AutoSeatHold says why the daemon must not start, rehydrate or relaunch an
// auto-spawned seat on its own (🎯T1066), or "" when nothing holds it. Two
// holds, checked in order: the frontier pause, then a target the ledger
// positively reads as closed. Only frontier-consume's own seats are held — a
// named worker or PO is never held here, and an explicit jevons_agent_start
// does not consult this.
//
// missionOpen is the ledger seam (NewLedgerMissionOpen). It answers false
// only on a positive reading, so an unreadable ledger or unbound target
// leaves the seat unheld rather than silently stranding real work.
func AutoSeatHold(name, targetID string, paused bool, missionOpen func(targetID string) bool) string {
	if !IsAutoSpawnedWorkerName(name) {
		return ""
	}
	if paused {
		return AutoHoldPaused
	}
	tid := strings.TrimSpace(targetID)
	if tid != "" && missionOpen != nil && !missionOpen(tid) {
		return AutoHoldTargetAchieved
	}
	return ""
}

// autoSeatHold is AutoSeatHold for a registered seat, read from this
// server's config pause, registry row and ledger hook.
func (s *Server) autoSeatHold(name string) string {
	if s == nil {
		return ""
	}
	targetID := ""
	if s.registry != nil {
		if d := s.registry.Def(name); d != nil {
			targetID = d.TargetID
		}
	}
	return AutoSeatHold(name, targetID, s.AutoSpawnPaused(), s.missionOpenHook())
}

// AutoSeatHold is the exported form the daemon's boot reattach consults.
func (s *Server) AutoSeatHold(name string) string { return s.autoSeatHold(name) }

// noteRevive records, durably, a daemon-initiated start of an existing seat:
// a rehydrate that ran, or one a hold refused. path names the revival road
// (send, dead_handle, reattach, plan_auth, recover). A mint is recorded by
// jevons_agent_start as agent_lifecycle.start; this is never that, so the two
// cannot be confused when an incident is read back (🎯T1066).
func (s *Server) noteRevive(name, path, hold string, err error) {
	if s == nil {
		return
	}
	fields := map[string]any{"name": name, "path": path, "how": "rehydrate"}
	switch {
	case hold != "":
		fields["reason"] = hold
		s.logLifecycle(compAgentLifecycle, "revive_held", "skipped", fields)
	case err != nil:
		fields["err"] = err.Error()
		s.logLifecycle(compAgentLifecycle, "rehydrate", "error", fields)
	default:
		s.logLifecycle(compAgentLifecycle, "rehydrate", "ok", fields)
	}
}

// noteDeadHandleRevives records each dead-handle relaunch the sweep ran or a
// hold refused, so the incident log names the road a seat came back by.
func (s *Server) noteDeadHandleRevives(reps []DeadAgentReport) {
	for _, r := range reps {
		switch {
		case r.Declined == AutoHoldPaused || r.Declined == AutoHoldTargetAchieved:
			s.noteRevive(r.Name, "dead_handle", r.Declined, nil)
		case r.Recovered:
			s.noteRevive(r.Name, "dead_handle", "", nil)
		}
	}
}

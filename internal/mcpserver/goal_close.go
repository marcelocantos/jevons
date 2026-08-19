// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
)

// clearSessionGoalIfComplete clears AgentDef.Goal and closes the live
// Session Goal when the mission is evidenced complete (🎯T528):
// ledger achieve of every TargetID named in the Goal, and/or an exact
// GOAL_STATUS: complete/blocked line in the terminal turn.
//
// Clearing the durable Goal is load-bearing: T510 keeps Goal across remint,
// and a remint with Goal still set reopens Continue even after GOAL_STATUS.
func (s *Server) clearSessionGoalIfComplete(name, turnText string) {
	if s == nil || s.registry == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	def := s.registry.Def(name)
	if def == nil || strings.TrimSpace(def.Goal) == "" {
		return
	}
	goal := def.Goal
	reason := ""
	if _, ok := claudia.ParseGoalStatus(turnText); ok {
		reason = "goal_status"
	} else {
		statuses := fleet.LoadGoalTargetStatuses(def.WorkDir, goal)
		if fleet.GoalMissionEvidencedComplete(goal, statuses) {
			reason = "ledger_achieved"
		}
	}
	if reason == "" {
		return
	}
	s.closeSessionGoal(name, def, reason)
}

// closeSessionGoal clears durable Goal and stops live continuation.
func (s *Server) closeSessionGoal(name string, def *claudia.AgentDef, reason string) {
	if def == nil {
		return
	}
	cleared := *def
	cleared.Goal = ""
	if err := s.registry.Register(cleared); err != nil {
		slog.Warn("T528 clear AgentDef.Goal failed", "agent", name, "reason", reason, "err", err)
	}
	if proc := s.registry.Get(name); proc != nil {
		proc.CloseGoal()
	}
	slog.Info("session goal closed", "agent", name, "reason", reason)
}

// wireSessionGoalCompleteCheck installs the ledger GoalCompleteCheck on a
// live Agent so maybeContinueGoal closes without injecting Continue when
// every named TargetID is achieved (🎯T528). Idempotent.
func (s *Server) wireSessionGoalCompleteCheck(name string, proc *claudia.Agent) {
	if s == nil || proc == nil || s.registry == nil {
		return
	}
	name = strings.TrimSpace(name)
	reg := s.registry
	proc.SetGoalCompleteCheck(func(goal, turnText string) bool {
		if _, ok := claudia.ParseGoalStatus(turnText); ok {
			return true
		}
		def := reg.Def(name)
		cwd := ""
		if def != nil {
			cwd = def.WorkDir
		}
		statuses := fleet.LoadGoalTargetStatuses(cwd, goal)
		if !fleet.GoalMissionEvidencedComplete(goal, statuses) {
			return false
		}
		// Persist clear so remint cannot reopen Continue.
		if def != nil && strings.TrimSpace(def.Goal) != "" {
			cleared := *def
			cleared.Goal = ""
			_ = reg.Register(cleared)
		}
		return true
	})
}

// effectiveSessionGoal returns def.Goal unless the ledger already evidences
// the mission complete — then empty so Launch does not start Continue (🎯T528).
func effectiveSessionGoal(def *claudia.AgentDef) string {
	if def == nil {
		return ""
	}
	goal := strings.TrimSpace(def.Goal)
	if goal == "" {
		return ""
	}
	statuses := fleet.LoadGoalTargetStatuses(def.WorkDir, goal)
	if fleet.GoalMissionEvidencedComplete(goal, statuses) {
		return ""
	}
	return goal
}

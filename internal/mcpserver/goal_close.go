// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
)

// missionComplete answers "is this seat's mission evidenced complete?" for
// every Session Goal seam in this file (🎯T528 / 🎯T738).
//
// def may be nil. The mission is def.TargetID when the registry holds one
// (🎯T198); otherwise the Goal text is scanned as before. Loading statuses
// goes through LoadGoalMissionStatuses so the bound id is read even when the
// brief never names it.
func missionComplete(def *claudia.AgentDef, goal string) bool {
	cwd, bound := "", ""
	if def != nil {
		cwd, bound = def.WorkDir, def.TargetID
	}
	statuses := fleet.LoadGoalMissionStatuses(cwd, bound, goal)
	return fleet.GoalMissionComplete(bound, goal, statuses)
}

// clearSessionGoalIfComplete clears AgentDef.Goal and closes the live
// Session Goal when the mission is evidenced complete (🎯T528):
// ledger achieve of the seat's bound target, and/or an exact
// GOAL_STATUS: complete/blocked line in the terminal turn.
//
// Clearing the durable Goal is load-bearing: T510 keeps Goal across remint,
// and a remint with Goal still set reopens Continue even after GOAL_STATUS.
//
// turnText arrives from this daemon's own per-seat accumulator
// (agentEventSink), not from claudia's goalTurn, which is why the
// GOAL_STATUS branch survives 🎯T738's second mechanism: a Grok ACP turn
// publishes its text as agent_message_chunk events and then terminates with
// a prompt result carrying no Text at all. The sink has already concatenated
// the chunks by the time the terminal stop flushes, so the marker is here
// even when claudia's own view of the turn is empty — and this runs before
// claudia's settle timer can inject a continuation.
func (s *Server) clearSessionGoalIfComplete(name, turnText string) {
	if s == nil || s.registry == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	def := s.registry.Def(name)
	if def == nil {
		// 🎯T738: the registry no longer knows this seat — it was reaped or
		// removed while its pane kept running. An unregistered seat has no
		// mission, so nothing it emits should earn another "Continue the
		// open objective". Close the live Goal rather than returning and
		// leaving the loop to run until the pane is killed by hand.
		s.closeOrphanSessionGoal(name)
		return
	}
	if strings.TrimSpace(def.Goal) == "" {
		return
	}
	goal := def.Goal
	reason := ""
	if _, ok := claudia.ParseGoalStatus(turnText); ok {
		reason = "goal_status"
	} else if missionComplete(def, goal) {
		reason = "ledger_achieved"
	}
	if reason == "" {
		return
	}
	s.closeSessionGoal(name, def, reason)
}

// closeOrphanSessionGoal stops host continuation for a live process whose
// registry row is gone (🎯T738). There is no def to clear, so this is the
// live half of closeSessionGoal on its own.
func (s *Server) closeOrphanSessionGoal(name string) {
	if s == nil || s.registry == nil {
		return
	}
	proc := s.registry.Get(name)
	if proc == nil || !proc.GoalActive() {
		return
	}
	proc.CloseGoal()
	slog.Info("session goal closed", "agent", name, "reason", "unregistered_seat")
}

// closeSeatGoalBeforeRemoval stops host continuation for a seat that is
// about to leave the registry (🎯T738).
//
// The reap path stops and Removes the agent, and nothing in it closed the
// Goal. When the kill did not actually take the pane down, what remained was
// a process whose Goal was still open and whose owner grant was gone — and
// claudia's askOwnerGoalCheck reads "no owner" as "not complete", so the
// daemon-held seat re-injected Continue forever. That is the replay storm in
// jv-t717-replay-digest, jv-t727-echo-recurrence, jv-t719-empty-run and
// jv-t718-gate-dirty-warn. Closing the Goal first makes the answer no longer
// depend on the owner still being there to give it.
//
// CloseGoal reaches the daemon-held seat through the broker backend's
// closeGoal op, so this is not merely local bookkeeping.
func (s *Server) closeSeatGoalBeforeRemoval(name, reason string) {
	if s == nil || s.registry == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if def := s.registry.Def(name); def != nil && strings.TrimSpace(def.Goal) != "" {
		cleared := *def
		cleared.Goal = ""
		if err := s.registry.Register(cleared); err != nil {
			slog.Warn("T738 clear AgentDef.Goal before removal failed",
				"agent", name, "reason", reason, "err", err)
		}
	}
	if proc := s.registry.Get(name); proc != nil && proc.GoalActive() {
		proc.CloseGoal()
		slog.Info("session goal closed", "agent", name, "reason", reason)
	}
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
// live Agent so maybeContinueGoal closes without injecting Continue when the
// seat's bound target is achieved (🎯T528 / 🎯T738). Idempotent.
//
// For a broker-held seat this check is what claudia's daemon asks over the
// wire (askOwnerGoalCheck → TypeGoalCheck → here), so it is the last gate in
// front of a continuation.
func (s *Server) wireSessionGoalCompleteCheck(name string, proc *claudia.Agent) {
	if s == nil || proc == nil || s.registry == nil {
		return
	}
	name = strings.TrimSpace(name)
	proc.SetGoalCompleteCheck(func(goal, turnText string) bool {
		return s.goalCheckVerdict(name, goal, turnText)
	})
}

// goalCheckVerdict is the answer wireSessionGoalCompleteCheck gives, split
// out so it can be exercised without a live Agent. True means "do not inject
// Continue".
func (s *Server) goalCheckVerdict(name, goal, turnText string) bool {
	if _, ok := claudia.ParseGoalStatus(turnText); ok {
		return true
	}
	def := s.registry.Def(name)
	if def == nil {
		// 🎯T738: no registry row, no mission. Answering false here is what
		// kept reaped-but-surviving panes on the continuation loop.
		return true
	}
	if !missionComplete(def, goal) {
		return false
	}
	// Persist clear so remint cannot reopen Continue.
	if strings.TrimSpace(def.Goal) != "" {
		cleared := *def
		cleared.Goal = ""
		_ = s.registry.Register(cleared)
	}
	return true
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
	if missionComplete(def, goal) {
		return ""
	}
	return goal
}

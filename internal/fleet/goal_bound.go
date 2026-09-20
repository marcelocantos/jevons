// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/targetfile"
)

// normalizeGoalTargetID turns "🎯t27.2" / " T27.2 " into "T27.2".
func normalizeGoalTargetID(id string) string {
	return strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "🎯")))
}

// goalStatusFor reads statusByID tolerantly: callers that forgot to
// normalise their keys still get an answer.
func goalStatusFor(statusByID map[string]string, id string) (string, bool) {
	if statusByID == nil || id == "" {
		return "", false
	}
	if st, ok := statusByID[id]; ok {
		return st, true
	}
	st, ok := statusByID[strings.ToLower(id)]
	return st, ok
}

// GoalMissionComplete reports whether the mission a seat carries is
// evidenced complete in the ledger (🎯T738).
//
// boundTargetID is AgentDef.TargetID: the one target the seat was minted
// against (🎯T198), and the same id 🎯T165's ledger-achieve reap already
// uses to decide the seat is finished. When it is set it IS the mission,
// and every other TargetID in goalText is prior art rather than scope.
//
// That single change is the whole of 🎯T738's first mechanism. AgentDef.Goal
// is the spawn brief verbatim — WorkSessionGoal returns the prompt unchanged
// — and every brief in this house cites half a dozen targets in its
// house-rules paragraph (🎯T377, 🎯T376, 🎯T712, 🎯T690). Requiring all of
// them closed meant no brief written in that style could ever evidence
// completion, because at least one cited target is always open. The specimen
// is the leftover pane jv-t717-replay-digest, which kept receiving "Continue
// the open objective" after 🎯T717 was achieved, because a house-rule
// sentence in its Goal named the still-open 🎯T710.
//
// A seat legitimately spanning several targets carries one of them (or an
// umbrella parent) as TargetID, so it now closes when that id closes while
// siblings are open. That is deliberate: the reaper decides the seat is done
// from this same id, and Goal closure disagreeing with the reaper is exactly
// how a pane outlives its mission and keeps burning. A seat that must stay
// open across independent targets leaves TargetID unset and falls back to
// the scan below, which is the branch where "all ids named must close" is
// still the right reading — with no bound id it is the only signal there is.
func GoalMissionComplete(boundTargetID, goalText string, statusByID map[string]string) bool {
	id := normalizeGoalTargetID(boundTargetID)
	if id == "" {
		return GoalMissionEvidencedComplete(goalText, statusByID)
	}
	st, ok := goalStatusFor(statusByID, id)
	return ok && targetfile.IsClosedStatus(st)
}

// GoalMissionContinueAllowed reports whether the host should still inject
// "Continue the open objective" for a seat bound to boundTargetID.
func GoalMissionContinueAllowed(boundTargetID, goalText string, statusByID map[string]string) bool {
	return !GoalMissionComplete(boundTargetID, goalText, statusByID)
}

// LoadGoalMissionStatuses loads ledger status for the bound target and for
// every TargetID named in goalText, under the nearest bullseye.yaml from cwd.
//
// The bound id is looked up whether or not the Goal text happens to mention
// it: a spawn brief addressed to a seat does not have to name that seat's
// target, and LoadGoalTargetStatuses alone would then return a map with no
// entry for the one id that decides the mission.
func LoadGoalMissionStatuses(cwd, boundTargetID, goalText string) map[string]string {
	out := LoadGoalTargetStatuses(cwd, goalText)
	id := normalizeGoalTargetID(boundTargetID)
	if id == "" {
		return out
	}
	if _, ok := goalStatusFor(out, id); ok {
		return out
	}
	st, ok := targetfile.LoadTargetStatusFromCwd(cwd, id)
	if !ok {
		return out
	}
	if out == nil {
		out = make(map[string]string, 1)
	}
	out[id] = st
	return out
}

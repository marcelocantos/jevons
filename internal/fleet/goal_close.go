// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/internal/targetfile"
)

// goalTargetIDRe matches 🎯T12 / T12 / T12.3 style target ids in a Goal
// or open-objective text (🎯T528).
var goalTargetIDRe = regexp.MustCompile(`(?i)(?:🎯)?T\d+(?:\.\d+)*`)

// GoalTargetIDs returns normalised TargetIDs (T512, T27.2) named in text.
func GoalTargetIDs(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, m := range goalTargetIDRe.FindAllString(text, -1) {
		id := strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(m), "🎯"))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// GoalMissionEvidencedComplete reports whether text names ≥1 TargetID and
// every named id is closed in statusByID (achieved or set_aside).
//
// Missing map entries and still-open statuses (identified / converging)
// leave the mission incomplete so Session Goal Continue stays allowed.
// A Goal that names no TargetIDs cannot close from the ledger alone
// (GOAL_STATUS / answered_or_closed remain the paths).
func GoalMissionEvidencedComplete(text string, statusByID map[string]string) bool {
	ids := GoalTargetIDs(text)
	if len(ids) == 0 || statusByID == nil {
		return false
	}
	for _, id := range ids {
		st, ok := statusByID[id]
		if !ok {
			// Also accept lowercase keys from callers that forgot to normalise.
			st, ok = statusByID[strings.ToLower(id)]
		}
		if !ok || !targetfile.IsClosedStatus(st) {
			return false
		}
	}
	return true
}

// SessionGoalContinueAllowed reports whether the host should still inject
// "Continue the open objective" for goalText given ledger statuses (🎯T528).
// False when every TargetID named in the Goal is evidenced complete.
func SessionGoalContinueAllowed(goalText string, statusByID map[string]string) bool {
	return !GoalMissionEvidencedComplete(goalText, statusByID)
}

// LoadGoalTargetStatuses looks up ledger status for every TargetID named in
// goalText under the nearest bullseye.yaml from cwd. Unknown ids are omitted
// (GoalMissionEvidencedComplete treats omission as incomplete).
func LoadGoalTargetStatuses(cwd, goalText string) map[string]string {
	ids := GoalTargetIDs(goalText)
	if len(ids) == 0 {
		return nil
	}
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		if st, ok := targetfile.LoadTargetStatusFromCwd(cwd, id); ok {
			out[id] = st
		}
	}
	return out
}

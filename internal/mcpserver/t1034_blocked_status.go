// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// reportOpenStatus is a one-way veto on destructive completion inference.
// A parsed status slot is the worker's explicit state, even if a different
// required slot makes the envelope invalid. GOAL_STATUS is read only from the
// worker's own (non-quoted/non-fenced) prose, not from a cited instruction.
func reportOpenStatus(report string) WorkerIdleAction {
	if m, _ := envelope.Parse(report); m != nil && m.Kind == envelope.KindFinishReport {
		if m.Status == envelope.ProgressBlocked {
			return IdleActionPark
		}
		if m.Status == envelope.ProgressInProgress {
			// The goal marker below may give a more specific external block.
			if !hasOwnBlockedGoalStatus(report) {
				return IdleActionKeep
			}
		}
	}
	if hasOwnBlockedGoalStatus(report) {
		return IdleActionPark
	}
	return ""
}

func hasOwnBlockedGoalStatus(report string) bool {
	b := []byte(strings.ToLower(report))
	maskFencedCode(b)
	maskInlineCode(b)
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "goal_status: blocked" {
			return true
		}
	}
	return false
}
